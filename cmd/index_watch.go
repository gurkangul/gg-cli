package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/graph"
	"github.com/gurkangul/gg-cli/internal/index/changed"
	"github.com/gurkangul/gg-cli/internal/index/runner"
	"github.com/gurkangul/gg-cli/internal/index/state"
)

const indexWatchLockFile = "index-watch.lock"

var (
	indexWatchPoll     time.Duration
	indexWatchDebounce time.Duration
	indexWatchOnce     bool
)

type indexWatchLock struct {
	PID       int    `json:"pid"`
	StartedAt string `json:"started_at"`
	Lang      string `json:"lang"`
	Root      string `json:"root"`
}

func init() {
	indexCmd.Flags().DurationVar(&indexWatchPoll, "watch-poll", time.Second, "foreground watch poll interval")
	indexCmd.Flags().DurationVar(&indexWatchDebounce, "watch-debounce", 2*time.Second, "foreground watch debounce before indexing")
	indexCmd.Flags().BoolVar(&indexWatchOnce, "watch-once", false, "run one watch poll then exit (test hook)")
	_ = indexCmd.Flags().MarkHidden("watch-once")
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

	fmt.Fprintf(cmd.OutOrStderr(), "gg index --watch - foreground watcher started (Ctrl-C stops it, lang=%s)\n", lang)
	ticker := time.NewTicker(indexWatchPoll)
	defer ticker.Stop()

	for {
		if err := runIndexWatchTick(cmd.Context(), cmd, cfg, root, ggDir, lang, r); err != nil {
			fmt.Fprintf(cmd.OutOrStderr(), "watch index failed: %v\n", err)
			fmt.Fprintln(cmd.OutOrStderr(), "recovery: check gg index status, gg doctor, Memgraph/Qdrant containers, then leave this foreground watcher running")
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

func runIndexWatchTick(parent context.Context, cmd *cobra.Command, cfg *config.Config, root, ggDir string, lang runner.Lang, r runner.Runner) error {
	needsIndex, full, err := indexWatchNeedsRun(parent, root, ggDir, lang)
	if err != nil {
		return err
	}
	if !needsIndex {
		return nil
	}
	timer := time.NewTimer(indexWatchDebounce)
	defer timer.Stop()
	select {
	case <-parent.Done():
		return nil
	case <-timer.C:
	}

	gc, err := graph.New(&cfg.Memgraph, cfg.ProjectID)
	if err != nil {
		return serviceErr(fmt.Sprintf("memgraph client: %v", err))
	}
	defer func() { _ = gc.Close(parent) }()
	ctx, cancel := context.WithTimeout(parent, 10*time.Minute)
	defer cancel()
	if err := gc.SchemaInit(ctx); err != nil {
		return fmt.Errorf("schema init: %w", err)
	}
	if full {
		fmt.Fprintln(cmd.OutOrStderr(), "watch: full index required - running full index")
		return runFullIndex(ctx, root, ggDir, lang, r, gc)
	}
	fmt.Fprintln(cmd.OutOrStderr(), "watch: source changes detected - running gg index --changed")
	return runChangedIndex(ctx, cmd, root, ggDir, lang, r, gc)
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
