package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/graph"
	"github.com/gurkangul/gg-cli/internal/index/changed"
	"github.com/gurkangul/gg-cli/internal/index/runner"
	"github.com/gurkangul/gg-cli/internal/index/state"
	"github.com/gurkangul/gg-cli/internal/index/watchresilience"
)

const indexWatchLockFile = "index-watch.lock"

// fullIndexCoalesceKey is the sentinel debounce key for a full-index trigger
// (state missing / non-ancestor / module-manifest change). It lets a burst of
// full-index triggers coalesce just like a burst of per-file source edits.
const fullIndexCoalesceKey = "<full-index>"

var (
	indexWatchPoll             time.Duration
	indexWatchDebounce         time.Duration
	indexWatchOpTimeout        time.Duration
	indexWatchFailureThreshold int
	indexWatchBreakerCooldown  time.Duration
	indexWatchOnce             bool
)

type indexWatchLock struct {
	PID       int    `json:"pid"`
	StartedAt string `json:"started_at"`
	Lang      string `json:"lang"`
	Root      string `json:"root"`
}

func init() {
	indexCmd.Flags().DurationVar(&indexWatchPoll, "watch-poll", time.Second, "foreground watch poll interval")
	indexCmd.Flags().DurationVar(&indexWatchDebounce, "watch-debounce", watchresilience.DefaultDebounce, "coalesce a burst of saves into one incremental reindex (debounce quiescence window)")
	indexCmd.Flags().DurationVar(&indexWatchOpTimeout, "watch-op-timeout", 10*time.Minute, "per-reindex watchdog timeout; a wedged indexer is logged+skipped, the watcher keeps running")
	indexCmd.Flags().IntVar(&indexWatchFailureThreshold, "watch-failure-threshold", watchresilience.DefaultFailureThreshold, "consecutive reindex failures before the circuit breaker pauses reindexing")
	indexCmd.Flags().DurationVar(&indexWatchBreakerCooldown, "watch-breaker-cooldown", watchresilience.DefaultCooldown, "how long the circuit breaker pauses reindexing after it trips")
	indexCmd.Flags().BoolVar(&indexWatchOnce, "watch-once", false, "run one watch poll then exit (test hook)")
	_ = indexCmd.Flags().MarkHidden("watch-once")
}

// indexWatchLoop holds the foreground resilience state shared across poll
// ticks. It is driven from a single goroutine (the watch loop) — gg is
// no-daemon, so nothing here outlives the foreground command.
type indexWatchLoop struct {
	cfg  *config.Config
	root string
	gg   string
	lang runner.Lang
	r    runner.Runner

	// singleFlight serializes overlapping reindex triggers. The poll loop is
	// already single-goroutine, but the guard makes serialization explicit and
	// defends against any future re-entrancy (TryLock => never blocks the loop).
	singleFlight sync.Mutex
	debounce     *watchresilience.Debouncer
	breaker      *watchresilience.Breaker
}

func runIndexWatch(cmd *cobra.Command) error {
	cfg, err := config.Load()
	if err != nil {
		return configErr(fmt.Sprintf("load config: %v", err))
	}
	root, err := config.FindRoot()
	if err != nil {
		return configErr(err.Error())
	}
	ggDir := filepath.Join(root, ".gg")
	lang := runner.Lang(indexLang)
	reg := runner.DefaultRegistry()
	r, ok := reg.Get(lang)
	if !ok {
		return fmt.Errorf("unsupported language %q - use %s", indexLang, strings.Join(langNames(runner.SupportedLangs()), ", "))
	}

	release, err := acquireIndexWatchLock(ggDir, indexWatchLock{
		PID:       os.Getpid(),
		StartedAt: time.Now().UTC().Format(time.RFC3339),
		Lang:      string(lang),
		Root:      root,
	})
	if err != nil {
		return err
	}
	defer release()

	loop := &indexWatchLoop{
		cfg:      cfg,
		root:     root,
		gg:       ggDir,
		lang:     lang,
		r:        r,
		debounce: watchresilience.NewDebouncer(indexWatchDebounce, nil),
		breaker:  watchresilience.NewBreaker(indexWatchFailureThreshold, indexWatchBreakerCooldown, nil),
	}

	fmt.Fprintf(cmd.OutOrStderr(), "gg index --watch - foreground watcher started (Ctrl-C stops it, lang=%s)\n", lang)
	fmt.Fprintf(cmd.OutOrStderr(), "resilience: serialized single-flight, %s debounce, %s per-op watchdog, circuit breaker trips after %d consecutive failures (%s cooldown)\n",
		indexWatchDebounce, indexWatchOpTimeout, indexWatchFailureThreshold, indexWatchBreakerCooldown)
	ticker := time.NewTicker(indexWatchPoll)
	defer ticker.Stop()

	for {
		if err := loop.tick(cmd.Context(), cmd); err != nil {
			fmt.Fprintf(cmd.OutOrStderr(), "watch index failed: %v\n", err)
			fmt.Fprintln(cmd.OutOrStderr(), "recovery: check gg index status, gg doctor, then leave this foreground watcher running")
		}
		if indexWatchOnce {
			return nil
		}
		select {
		case <-cmd.Context().Done():
			return nil
		case <-ticker.C:
		}
	}
}

// tick runs one poll iteration: detect changes, coalesce a burst via the
// debouncer, gate on the circuit breaker, then run one serialized reindex
// under a per-op watchdog. It never returns a fatal error for an indexer
// failure/timeout — those are logged, fed to the breaker, and the loop
// continues. A non-nil return is only an environment-level error (bad config,
// git failure) the outer loop reports without exiting.
func (l *indexWatchLoop) tick(parent context.Context, cmd *cobra.Command) error {
	needsIndex, full, err := indexWatchNeedsRun(parent, l.root, l.gg, l.lang)
	if err != nil {
		return err
	}
	if !needsIndex {
		l.debounce.Reset()
		return nil
	}

	// Coalesce: feed the changed set to the debouncer. A burst of saves keeps
	// resetting the quiescence timer; only a settled set fires the reindex.
	keys, full := l.coalesceKeys(parent, full)
	if !l.debounce.Observe(keys) {
		return nil // still coalescing this burst
	}

	// Circuit breaker: after repeated failures, pause reindexing for a cooldown
	// instead of hammering a wedged indexer every poll.
	if !l.breaker.Allow() {
		snap := l.breaker.Snapshot()
		fmt.Fprintf(cmd.OutOrStderr(), "watch: circuit breaker open - %s\n", snap.OpenError())
		return nil
	}

	// Single-flight: never overlap two reindex runs against one graph.db. The
	// loop is single-goroutine so this always succeeds today; TryLock keeps the
	// poll non-blocking if that ever changes.
	if !l.singleFlight.TryLock() {
		fmt.Fprintln(cmd.OutOrStderr(), "watch: reindex already in flight - skipping this tick")
		return nil
	}
	defer l.singleFlight.Unlock()

	runErr := l.runReindex(parent, cmd, full)
	if runErr != nil {
		// Caller-initiated cancellation (Ctrl-C) is not an indexer failure. The
		// breaker contract (watchresilience/breaker.go) says RecordFailure must
		// not be fed caller cancellation, so skip both Record* calls and let the
		// outer loop observe ctx.Done() and exit cleanly — no spurious breaker trip.
		if errors.Is(runErr, context.Canceled) {
			return nil
		}
		if errors.Is(runErr, context.DeadlineExceeded) {
			fmt.Fprintf(cmd.OutOrStderr(), "watch: reindex exceeded %s watchdog - skipped, watcher still running\n", indexWatchOpTimeout)
		} else {
			fmt.Fprintf(cmd.OutOrStderr(), "watch: reindex failed: %v\n", runErr)
		}
		l.breaker.RecordFailure(runErr)
		// Drop the coalesced set so the next attempt is paced by the breaker /
		// next real change, not retried every poll behind the same failure.
		l.debounce.Reset()
		if snap := l.breaker.Snapshot(); snap.State == watchresilience.BreakerOpen {
			fmt.Fprintf(cmd.OutOrStderr(), "watch: circuit breaker tripped - %s\n", snap.OpenError())
		}
		return nil
	}
	l.breaker.RecordSuccess()
	l.debounce.Reset()
	return nil
}

// coalesceKeys returns the debounce coalescing keys for the current change set.
// For a full-index trigger it returns a single sentinel key; for an incremental
// trigger it returns the language-relative changed file paths so per-file
// bursts coalesce. If the changed-file probe fails it degrades to the sentinel
// (still coalesces, never crashes the watcher) and forces a full pass.
func (l *indexWatchLoop) coalesceKeys(ctx context.Context, full bool) ([]string, bool) {
	if full {
		return []string{fullIndexCoalesceKey}, true
	}
	baseSHA, ok := indexWatchBaseSHA(l.gg, l.lang)
	if !ok {
		return []string{fullIndexCoalesceKey}, true
	}
	files, err := changed.Files(ctx, l.root, baseSHA, langExtensions(l.lang))
	if err != nil || len(files) == 0 {
		// No discrete file list to key on — coalesce under the sentinel.
		return []string{fullIndexCoalesceKey}, full
	}
	keys := make([]string, 0, len(files))
	for _, f := range files {
		if rel, relErr := relForwardSlash(l.root, f); relErr == nil {
			keys = append(keys, rel)
		} else {
			keys = append(keys, filepath.ToSlash(f))
		}
	}
	return keys, false
}

// indexWatchBaseSHA reads the last-indexed base SHA for the language, if any.
func indexWatchBaseSHA(ggDir string, lang runner.Lang) (string, bool) {
	s, err := state.Read(ggDir)
	if err != nil {
		return "", false
	}
	langState, ok := s.ForLanguage(string(lang))
	if !ok || langState.LastIndexedSHA == "" || langState.LastIndexedSHA == changed.EmptyTreeSHA {
		return "", false
	}
	return langState.LastIndexedSHA, true
}

// runReindex opens the graph client and runs one full or incremental reindex
// under the per-op watchdog timeout. The timeout is the resilience backstop: a
// wedged indexer surfaces as context.DeadlineExceeded, which tick() logs and
// feeds to the breaker rather than letting it hang the poll loop forever.
func (l *indexWatchLoop) runReindex(parent context.Context, cmd *cobra.Command, full bool) error {
	gc, err := graph.New(l.cfg.DataDir, l.cfg.ProjectID)
	if err != nil {
		return serviceErr(fmt.Sprintf("code graph client: %v", err))
	}
	defer func() { _ = gc.Close(parent) }()

	ctx, cancel := context.WithTimeout(parent, indexWatchOpTimeout)
	defer cancel()
	if err := gc.SchemaInit(ctx); err != nil {
		return memgraphDownErr("schema init", err)
	}
	if full {
		fmt.Fprintln(cmd.OutOrStderr(), "watch: full index required - running full index")
		return runFullIndex(ctx, l.root, l.gg, l.lang, l.r, gc)
	}
	fmt.Fprintln(cmd.OutOrStderr(), "watch: source changes settled - running gg index --changed")
	return runChangedIndex(ctx, cmd, l.root, l.gg, l.lang, l.r, gc)
}

func indexWatchNeedsRun(ctx context.Context, root, ggDir string, lang runner.Lang) (bool, bool, error) {
	s, err := state.Read(ggDir)
	if errors.Is(err, state.ErrNoState) {
		return true, true, nil
	}
	if err != nil {
		return false, false, fmt.Errorf("read index state: %w", err)
	}
	langState, ok := s.ForLanguage(string(lang))
	if !ok || langState.LastIndexedSHA == "" {
		return true, true, nil
	}
	baseSHA := langState.LastIndexedSHA
	if baseSHA == changed.EmptyTreeSHA {
		return true, true, nil
	}
	ancestor, err := changed.IsAncestor(ctx, root, baseSHA)
	if err != nil {
		return false, false, fmt.Errorf("check index state ancestor: %w", err)
	}
	if !ancestor {
		return true, true, nil
	}
	exts := langState.Extensions
	if len(exts) == 0 {
		exts = langExtensions(lang)
	}
	manifests := manifestsForLang(lang)
	fingerprint, err := changed.WorkingTreeFingerprintWithNames(ctx, root, baseSHA, exts, manifests)
	if err != nil {
		return false, false, fmt.Errorf("compute current source/module fingerprint: %w", err)
	}
	if fingerprint == langState.WorkingTreeFingerprint {
		return false, false, nil
	}
	if langState.WorkingTreeFingerprint != "" {
		return true, true, nil
	}
	summary, err := codeGraphChangesSince(ctx, root, baseSHA, exts, manifests)
	if err != nil {
		return false, false, fmt.Errorf("compute change summary: %w", err)
	}
	if summary.ModuleFiles > 0 {
		return true, true, nil
	}
	return summary.hasChanges(), false, nil
}

func acquireIndexWatchLock(ggDir string, lock indexWatchLock) (func(), error) {
	path := filepath.Join(ggDir, indexWatchLockFile)
	if existing, ok := readIndexWatchLock(path); ok && processRunning(existing.PID) {
		return nil, fmt.Errorf("index watcher already running for this project (pid=%d, lang=%s, started=%s)", existing.PID, existing.Lang, existing.StartedAt)
	}
	_ = os.Remove(path)
	data, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, fmt.Errorf("create index watch lock: %w", err)
	}
	if _, err := f.Write(append(data, '\n')); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return nil, fmt.Errorf("write index watch lock: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return nil, fmt.Errorf("close index watch lock: %w", err)
	}
	released := false
	return func() {
		if released {
			return
		}
		released = true
		_ = os.Remove(path)
	}, nil
}

func readIndexWatchLock(path string) (indexWatchLock, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return indexWatchLock{}, false
	}
	var lock indexWatchLock
	if err := json.Unmarshal(data, &lock); err != nil {
		return indexWatchLock{}, false
	}
	return lock, lock.PID > 0
}

func processRunning(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}
