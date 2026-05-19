package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/graph"
	"github.com/gurkangul/gg-cli/internal/index/changed"
	"github.com/gurkangul/gg-cli/internal/index/runner"
	"github.com/gurkangul/gg-cli/internal/index/state"
	"github.com/gurkangul/gg-cli/internal/outbox"
)

var indexStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show code graph freshness and quality",
	RunE:  runIndexStatus,
}

func init() {
	indexCmd.AddCommand(indexStatusCmd)
}

type codeGraphStatus struct {
	Status                 string      `json:"status"`
	Detail                 string      `json:"detail,omitempty"`
	LastIndexedSHA         string      `json:"last_indexed_sha,omitempty"`
	HeadSHA                string      `json:"head_sha,omitempty"`
	IndexedAt              string      `json:"indexed_at,omitempty"`
	WorkingTreeFingerprint string      `json:"working_tree_fingerprint,omitempty"`
	MemgraphAvailable      bool        `json:"memgraph_available"`
	MemgraphDetail         string      `json:"memgraph_detail,omitempty"`
	GraphEmpty             bool        `json:"graph_empty"`
	Stats                  graph.Stats `json:"stats"`
	IndexedLanguages       []string    `json:"indexed_languages,omitempty"`
	PendingOutbox          int         `json:"pending_outbox"`
	NoWatcherStarted       bool        `json:"no_watcher_started"`
	Watcher                string      `json:"watcher,omitempty"`
}

func runIndexStatus(cmd *cobra.Command, _ []string) error {
	cfg, err := config.Load()
	if err != nil {
		return configErr(fmt.Sprintf("load config: %v", err))
	}
	root, err := config.FindRoot()
	if err != nil {
		return configErr(err.Error())
	}
	ggDir := root + "/.gg"

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	status := collectCodeGraphStatus(ctx, root, ggDir, cfg)
	return printJSON(status, func() {
		renderCodeGraphStatus(cmd.OutOrStdout(), status)
	})
}

func collectCodeGraphStatus(ctx context.Context, root, ggDir string, cfg *config.Config) codeGraphStatus {
	status := codeGraphStatus{
		Status:           "unknown",
		NoWatcherStarted: true,
	}

	if entries, err := outbox.List(ggDir); err == nil {
		status.PendingOutbox = len(entries)
	}

	detectedLangs, hasSupportedSource, detectErr := detectCodeGraphLanguages(root)
	if detectErr != nil {
		status.Status = "unknown"
		status.Detail = "codegraph applicability check failed: " + detectErr.Error()
		status.fillGraphStats(ctx, cfg)
		status.fillWatcher(ggDir)
		status.finalize()
		return status
	}
	if len(detectedLangs) == 0 && !hasSupportedSource {
		status.Status = "not_applicable"
		status.Detail = "no supported code modules or source files found"
		status.fillGraphStats(ctx, cfg)
		status.fillWatcher(ggDir)
		status.finalize()
		return status
	}

	if head, err := changed.HeadSHA(ctx, root); err == nil {
		status.HeadSHA = head
	} else {
		status.Status = "not_applicable"
		status.Detail = "git HEAD unavailable; CodeGraph requires a git repository"
		status.fillGraphStats(ctx, cfg)
		status.fillWatcher(ggDir)
		status.finalize()
		return status
	}

	var idxState *state.IndexState
	if s, err := state.Read(ggDir); err == nil {
		idxState = s
		status.LastIndexedSHA = s.LastIndexedSHA
		status.IndexedAt = s.IndexedAt
		status.WorkingTreeFingerprint = s.WorkingTreeFingerprint
		status.IndexedLanguages = s.IndexedLanguages()
	} else if errors.Is(err, state.ErrNoState) {
		status.Status = "missing"
		status.Detail = "index-state.json missing - run gg index --lang <lang>"
	} else {
		status.Status = "unknown"
		status.Detail = "index-state unreadable: " + err.Error()
	}

	status.fillGitFreshness(ctx, root, idxState, detectedLangs)
	status.fillGraphStats(ctx, cfg)
	status.fillWatcher(ggDir)
	status.finalize()
	return status
}

func (s *codeGraphStatus) fillGitFreshness(ctx context.Context, root string, idxState *state.IndexState, detectedLangs []runner.Lang) {
	if idxState == nil {
		return
	}
	if len(idxState.Languages) > 0 {
		s.fillLanguageGitFreshness(ctx, root, idxState, detectedLangs)
		return
	}
	s.fillLegacyGitFreshness(ctx, root)
}

func (s *codeGraphStatus) fillLegacyGitFreshness(ctx context.Context, root string) {
	if s.LastIndexedSHA == "" || s.HeadSHA == "" {
		return
	}
	if s.LastIndexedSHA == s.HeadSHA {
		fingerprint, err := changed.WorkingTreeFingerprint(ctx, root, s.LastIndexedSHA, codeGraphSourceExtensions())
		if err != nil {
			s.Status = "unknown"
			s.Detail = "working-tree freshness check failed: " + err.Error()
			return
		}
		if fingerprint != s.WorkingTreeFingerprint {
			s.Status = "stale"
			if fingerprint == "" {
				s.Detail = "working tree is clean but index-state records dirty indexed content - run gg index --changed"
			} else {
				s.Detail = "working tree has changed or untracked source files after last index - run gg index --changed"
			}
			return
		}
		s.Status = "ready"
		if fingerprint != "" {
			s.Detail = "index-state matches HEAD and indexed dirty working tree source fingerprint"
		} else {
			s.Detail = "index-state matches HEAD and working tree source files"
		}
		return
	}
	ancestor, err := changed.IsAncestor(ctx, root, s.LastIndexedSHA)
	if err != nil {
		s.Status = "unknown"
		s.Detail = "ancestor check failed: " + err.Error()
		return
	}
	if !ancestor {
		s.Status = "non_ancestor"
		s.Detail = "last indexed SHA is not an ancestor of HEAD - run full gg index"
		return
	}
	s.Status = "stale"
	s.Detail = "HEAD has commits after the last indexed SHA - run gg index --changed"
}

func (s *codeGraphStatus) fillLanguageGitFreshness(ctx context.Context, root string, idxState *state.IndexState, detectedLangs []runner.Lang) {
	if s.HeadSHA == "" {
		return
	}
	if len(detectedLangs) == 0 {
		detectedLangs = langsFromNames(idxState.IndexedLanguages())
	}
	var missing []string
	for _, lang := range detectedLangs {
		if _, ok := idxState.ForLanguage(string(lang)); !ok {
			missing = append(missing, string(lang))
		}
	}
	if len(missing) > 0 {
		s.Status = "stale"
		s.Detail = fmt.Sprintf("unindexed language(s): %s - run gg index --lang %s", strings.Join(missing, ", "), missing[0])
		return
	}
	dirtyIndexed := false
	for _, lang := range detectedLangs {
		entry, ok := idxState.ForLanguage(string(lang))
		if !ok {
			continue
		}
		exts := entry.Extensions
		if len(exts) == 0 {
			exts = langExtensions(lang)
		}
		if entry.LastIndexedSHA == s.HeadSHA {
			fingerprint, err := changed.WorkingTreeFingerprint(ctx, root, entry.LastIndexedSHA, exts)
			if err != nil {
				s.Status = "unknown"
				s.Detail = fmt.Sprintf("%s working-tree freshness check failed: %v", lang, err)
				return
			}
			if fingerprint != entry.WorkingTreeFingerprint {
				s.Status = "stale"
				if fingerprint == "" {
					s.Detail = fmt.Sprintf("%s working tree is clean but index-state records dirty indexed content - run gg index --lang %s --changed", lang, lang)
				} else {
					s.Detail = fmt.Sprintf("%s working tree has changed or untracked source files after last index - run gg index --lang %s --changed", lang, lang)
				}
				return
			}
			if fingerprint != "" {
				dirtyIndexed = true
			}
			continue
		}
		ancestor, err := changed.IsAncestor(ctx, root, entry.LastIndexedSHA)
		if err != nil {
			s.Status = "unknown"
			s.Detail = fmt.Sprintf("%s ancestor check failed: %v", lang, err)
			return
		}
		if !ancestor {
			s.Status = "non_ancestor"
			s.Detail = fmt.Sprintf("%s last indexed SHA is not an ancestor of HEAD - run full gg index --lang %s", lang, lang)
			return
		}
		s.Status = "stale"
		s.Detail = fmt.Sprintf("%s HEAD has commits after the last indexed SHA - run gg index --lang %s --changed", lang, lang)
		return
	}
	s.Status = "ready"
	if dirtyIndexed {
		s.Detail = "index-state matches HEAD and indexed dirty working tree source fingerprint"
	} else {
		s.Detail = "index-state matches HEAD and working tree source files"
	}
	if names := langNames(detectedLangs); len(names) > 0 {
		s.Detail += " for " + strings.Join(names, ", ")
	}
}

func (s *codeGraphStatus) fillGraphStats(ctx context.Context, cfg *config.Config) {
	if cfg == nil || cfg.Memgraph.URI == "" {
		s.MemgraphDetail = "not configured"
		return
	}
	gc, err := graph.New(&cfg.Memgraph, cfg.ProjectID)
	if err != nil {
		s.MemgraphDetail = "client init: " + err.Error()
		return
	}
	defer func() { _ = gc.Close(ctx) }()

	if err := gc.HealthCheck(ctx); err != nil {
		s.MemgraphDetail = "unavailable: " + err.Error()
		return
	}
	s.MemgraphAvailable = true
	s.MemgraphDetail = "reachable"

	stats, err := gc.Stats(ctx)
	if err != nil {
		s.MemgraphDetail = "stats unavailable: " + err.Error()
		return
	}
	s.Stats = stats
	s.GraphEmpty = stats.Files == 0 && stats.Symbols == 0 && stats.Edges == 0
}

func (s *codeGraphStatus) finalize() {
	if s.Status == "not_applicable" {
		return
	}
	if s.PendingOutbox > 0 {
		s.Status = "partial"
		if s.Detail == "" {
			s.Detail = "pending index outbox entries - run gg doctor --reconcile"
		}
	}
	if s.MemgraphAvailable && s.GraphEmpty {
		s.Status = "missing"
		s.Detail = "Memgraph is reachable but graph is empty - run gg index --lang <lang>"
	}
	if !s.MemgraphAvailable && s.Detail == "" {
		s.Detail = "Memgraph unavailable or not configured"
	}
}

func (s *codeGraphStatus) fillWatcher(ggDir string) {
	lock, ok := readIndexWatchLock(ggDir + "/" + indexWatchLockFile)
	if !ok {
		return
	}
	if !processRunning(lock.PID) {
		s.Watcher = fmt.Sprintf("stale pid=%d", lock.PID)
		return
	}
	s.NoWatcherStarted = false
	s.Watcher = fmt.Sprintf("running pid=%d lang=%s started=%s", lock.PID, lock.Lang, lock.StartedAt)
}

func renderCodeGraphStatus(w io.Writer, s codeGraphStatus) {
	fmt.Fprintln(w, "CODE GRAPH:")
	fmt.Fprintf(w, "  Status: %s", s.Status)
	if s.Detail != "" {
		fmt.Fprintf(w, " - %s", s.Detail)
	}
	fmt.Fprintln(w)
	if s.LastIndexedSHA != "" || s.HeadSHA != "" {
		fmt.Fprintf(w, "  SHA: indexed=%s head=%s\n", indexStatusShortSHA(s.LastIndexedSHA), indexStatusShortSHA(s.HeadSHA))
	}
	if s.IndexedAt != "" {
		fmt.Fprintf(w, "  Indexed: %s\n", s.IndexedAt)
	}
	if len(s.IndexedLanguages) > 0 {
		fmt.Fprintf(w, "  Languages: %s\n", strings.Join(s.IndexedLanguages, ", "))
	}
	fmt.Fprintf(w, "  Memgraph: %s", boolWord(s.MemgraphAvailable, "available", "unavailable"))
	if s.MemgraphDetail != "" {
		fmt.Fprintf(w, " (%s)", s.MemgraphDetail)
	}
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  Counts: files=%d symbols=%d edges=%d\n", s.Stats.Files, s.Stats.Symbols, s.Stats.Edges)
	if s.PendingOutbox > 0 {
		fmt.Fprintf(w, "  Outbox: %d pending index write(s)\n", s.PendingOutbox)
	}
	if s.NoWatcherStarted {
		fmt.Fprintln(w, "  Watcher: not started implicitly")
	} else {
		fmt.Fprintf(w, "  Watcher: %s\n", s.Watcher)
	}
}

func codeGraphSourceExtensions() []string {
	seen := make(map[string]bool)
	var out []string
	for _, lang := range []runner.Lang{runner.LangGo, runner.LangPython, runner.LangTypeScript} {
		for _, ext := range langExtensions(lang) {
			if !seen[ext] {
				seen[ext] = true
				out = append(out, ext)
			}
		}
	}
	return out
}

func detectCodeGraphLanguages(root string) ([]runner.Lang, bool, error) {
	var langs []runner.Lang
	for _, lang := range []runner.Lang{runner.LangGo, runner.LangPython, runner.LangTypeScript} {
		dirs, err := discoverModuleDirs(root, lang)
		if err != nil {
			return nil, false, err
		}
		if len(dirs) > 0 {
			langs = append(langs, lang)
		}
	}
	hasSource, err := hasCodeGraphSourceFiles(root)
	if err != nil {
		return nil, false, err
	}
	return langs, hasSource, nil
}

func hasCodeGraphSourceFiles(root string) (bool, error) {
	extSet := make(map[string]bool)
	for _, ext := range codeGraphSourceExtensions() {
		extSet[ext] = true
	}
	skipDirs := make(map[string]bool, len(config.DefaultHookInstallSkipDirs)+6)
	for _, name := range config.DefaultHookInstallSkipDirs {
		skipDirs[name] = true
	}
	for _, name := range []string{".git", config.DirName, "node_modules", "vendor", "dist", "build"} {
		skipDirs[name] = true
	}
	found := errors.New("codegraph source found")
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if extSet[filepath.Ext(d.Name())] {
			return found
		}
		return nil
	})
	if errors.Is(err, found) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return false, nil
}

func langsFromNames(names []string) []runner.Lang {
	langs := make([]runner.Lang, 0, len(names))
	for _, name := range names {
		langs = append(langs, runner.Lang(name))
	}
	sort.Slice(langs, func(i, j int) bool { return langs[i] < langs[j] })
	return langs
}

func langNames(langs []runner.Lang) []string {
	names := make([]string, 0, len(langs))
	for _, lang := range langs {
		names = append(names, string(lang))
	}
	sort.Strings(names)
	return names
}

func indexStatusShortSHA(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	if sha == "" {
		return "-"
	}
	return sha
}

func boolWord(ok bool, yes, no string) string {
	if ok {
		return yes
	}
	return no
}

func renderCodeGraphStatusCompact(s codeGraphStatus) string {
	var parts []string
	parts = append(parts, "CodeGraph "+s.Status)
	if s.LastIndexedSHA != "" || s.HeadSHA != "" {
		parts = append(parts, fmt.Sprintf("idx=%s head=%s", indexStatusShortSHA(s.LastIndexedSHA), indexStatusShortSHA(s.HeadSHA)))
	}
	if len(s.IndexedLanguages) > 0 {
		parts = append(parts, "langs="+strings.Join(s.IndexedLanguages, ","))
	}
	parts = append(parts, fmt.Sprintf("memgraph=%s", boolWord(s.MemgraphAvailable, "ok", "down")))
	parts = append(parts, fmt.Sprintf("files=%d sym=%d edges=%d", s.Stats.Files, s.Stats.Symbols, s.Stats.Edges))
	if s.PendingOutbox > 0 {
		parts = append(parts, fmt.Sprintf("outbox=%d", s.PendingOutbox))
	}
	if s.Detail != "" {
		parts = append(parts, compactTrim(s.Detail, 90))
	}
	if !s.NoWatcherStarted {
		parts = append(parts, "watch="+compactTrim(s.Watcher, 50))
	}
	return strings.Join(parts, "  ")
}

func codeGraphStatusWithTimeout(root, ggDir string, cfg *config.Config) codeGraphStatus {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return collectCodeGraphStatus(ctx, root, ggDir, cfg)
}
