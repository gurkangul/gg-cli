package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

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
	Long: `Show CodeGraph freshness using the shared agent-facing contract.

gg never runs a background index daemon. Repair is explicit with
gg doctor --fix-index; optional active foreground mode is gg index --watch or
gg watch --index.`,
	RunE: runIndexStatus,
}

func init() {
	indexCmd.AddCommand(indexStatusCmd)
}

type codeGraphStatus struct {
	Status                 string             `json:"status"`
	Detail                 string             `json:"detail,omitempty"`
	LastIndexedSHA         string             `json:"last_indexed_sha,omitempty"`
	HeadSHA                string             `json:"head_sha,omitempty"`
	IndexedAt              string             `json:"indexed_at,omitempty"`
	WorkingTreeFingerprint string             `json:"working_tree_fingerprint,omitempty"`
	MemgraphConfigured     bool               `json:"memgraph_configured"`
	MemgraphAvailable      bool               `json:"memgraph_available"`
	GraphStatsAvailable    bool               `json:"graph_stats_available"`
	MemgraphDetail         string             `json:"memgraph_detail,omitempty"`
	GraphEmpty             bool               `json:"graph_empty"`
	Stats                  graph.Stats        `json:"stats"`
	IndexedLanguages       []string           `json:"indexed_languages,omitempty"`
	DetectedLanguages      []string           `json:"detected_languages,omitempty"`
	SuggestedCommand       string             `json:"suggested_command,omitempty"`
	ChangedFiles           int                `json:"changed_files,omitempty"`
	NewFiles               int                `json:"new_files,omitempty"`
	DeletedFiles           int                `json:"deleted_files,omitempty"`
	ModuleFiles            int                `json:"module_files,omitempty"`
	PendingOutbox          int                `json:"pending_outbox"`
	NoWatcherStarted       bool               `json:"no_watcher_started"`
	Watcher                string             `json:"watcher,omitempty"`
	CodeGraph              codeGraphFreshness `json:"codegraph"`
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
		for _, entry := range entries {
			if entry.Kind == "full-index" || entry.Kind == "changed-index" {
				status.PendingOutbox++
			}
		}
	}

	detectedLangs, hasSupportedSource, detectErr := detectCodeGraphLanguages(root)
	status.DetectedLanguages = langNames(detectedLangs)
	status.SuggestedCommand = codeGraphIndexSuggestion(detectedLangs)
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
		status.Detail = codeGraphMissingDetail(detectedLangs, hasSupportedSource)
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
		summary, summaryErr := codeGraphChangesSince(ctx, root, s.LastIndexedSHA, codeGraphSourceExtensions(), codeGraphAllManifestNames())
		if summaryErr != nil {
			s.Status = "unknown"
			s.Detail = "working-tree change summary failed: " + summaryErr.Error()
			return
		}
		fingerprint, err := changed.WorkingTreeFingerprintWithNames(ctx, root, s.LastIndexedSHA, codeGraphSourceExtensions(), codeGraphAllManifestNames())
		if err != nil {
			s.Status = "unknown"
			s.Detail = "working-tree freshness check failed: " + err.Error()
			return
		}
		if fingerprint != s.WorkingTreeFingerprint {
			s.applyChangeSummary(summary)
			s.Status = "stale"
			if summary.hasChanges() {
				s.Detail = summary.detail("gg index --lang go")
			} else if fingerprint == "" {
				s.Detail = "working tree is clean but index-state records dirty indexed content - run gg index --lang go"
			} else {
				s.Detail = "working tree has changed or untracked source files after last index - run gg index --lang go"
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
	fingerprint, fpErr := changed.WorkingTreeFingerprintWithNames(ctx, root, s.LastIndexedSHA, codeGraphSourceExtensions(), codeGraphAllManifestNames())
	if fpErr == nil && fingerprint == s.WorkingTreeFingerprint {
		s.Status = "ready"
		if fingerprint != "" {
			s.Detail = "indexed dirty working tree source/module fingerprint matches current tree"
		} else {
			s.Detail = "index-state source/module fingerprint matches current tree"
		}
		return
	}
	summary, summaryErr := codeGraphChangesSince(ctx, root, s.LastIndexedSHA, codeGraphSourceExtensions(), codeGraphAllManifestNames())
	if summaryErr == nil {
		s.applyChangeSummary(summary)
		if summary.hasChanges() {
			s.Status = "stale"
			s.Detail = summary.detail("gg index --lang go --changed")
			return
		}
	}
	s.Status = "stale"
	s.Detail = "HEAD has commits after the last indexed SHA - run gg index --lang go --changed"
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
		s.Detail = fmt.Sprintf("unindexed language(s): %s; project gained code since last index - run %s", strings.Join(missing, ", "), codeGraphIndexSuggestion(langsFromNames(missing)))
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
		manifests := manifestsForLang(lang)
		command := fmt.Sprintf("gg index --lang %s --changed", lang)
		if entry.LastIndexedSHA == s.HeadSHA {
			summary, summaryErr := codeGraphChangesSince(ctx, root, entry.LastIndexedSHA, exts, manifests)
			if summaryErr != nil {
				s.Status = "unknown"
				s.Detail = fmt.Sprintf("%s working-tree change summary failed: %v", lang, summaryErr)
				return
			}
			fingerprint, err := changed.WorkingTreeFingerprintWithNames(ctx, root, entry.LastIndexedSHA, exts, manifests)
			if err != nil {
				s.Status = "unknown"
				s.Detail = fmt.Sprintf("%s working-tree freshness check failed: %v", lang, err)
				return
			}
			if fingerprint != entry.WorkingTreeFingerprint {
				s.applyChangeSummary(summary)
				s.Status = "stale"
				if summary.hasChanges() {
					s.Detail = summary.detail(command)
				} else if fingerprint == "" {
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
		fingerprint, fpErr := changed.WorkingTreeFingerprintWithNames(ctx, root, entry.LastIndexedSHA, exts, manifests)
		if fpErr == nil && fingerprint == entry.WorkingTreeFingerprint {
			if fingerprint != "" {
				dirtyIndexed = true
			}
			continue
		}
		summary, summaryErr := codeGraphChangesSince(ctx, root, entry.LastIndexedSHA, exts, manifests)
		if summaryErr == nil {
			s.applyChangeSummary(summary)
			if summary.hasChanges() {
				s.Status = "stale"
				s.Detail = summary.detail(command)
				return
			}
		}
		s.Status = "stale"
		s.Detail = fmt.Sprintf("%s HEAD has commits after the last indexed SHA - run gg index --lang %s", lang, lang)
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

func (s *codeGraphStatus) applyChangeSummary(summary codeGraphChangeSummary) {
	s.ChangedFiles += summary.ChangedFiles
	s.NewFiles += summary.NewFiles
	s.DeletedFiles += summary.DeletedFiles
	s.ModuleFiles += summary.ModuleFiles
}

func (s *codeGraphStatus) fillGraphStats(ctx context.Context, cfg *config.Config) {
	if cfg == nil {
		s.MemgraphDetail = "not checked"
		return
	}
	if cfg.Memgraph.URI == "" {
		s.MemgraphDetail = "not configured"
		return
	}
	s.MemgraphConfigured = true
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
	s.GraphStatsAvailable = true
	s.GraphEmpty = stats.Files == 0 && stats.Symbols == 0 && stats.Edges == 0
}

func (s *codeGraphStatus) finalize() {
	defer s.refreshFreshnessContract()
	if s.Status == "not_applicable" {
		return
	}
	if s.PendingOutbox > 0 {
		s.Status = "partial"
		msg := "pending index outbox entries - run gg doctor --reconcile"
		if s.Detail == "" {
			s.Detail = msg
		} else if !strings.Contains(s.Detail, "pending index outbox") {
			s.Detail += "; " + msg
		}
	}
	if s.MemgraphAvailable && s.GraphEmpty {
		s.Status = "missing"
		if fullCommand := codeGraphFullIndexSuggestionForStatus(*s); fullCommand != "" {
			s.SuggestedCommand = fullCommand
		}
		msg := "Memgraph projection is empty"
		if s.SuggestedCommand != "" {
			msg += " - run " + s.SuggestedCommand
		}
		if s.Detail == "" || strings.HasPrefix(s.Detail, "index-state matches") {
			s.Detail = msg
		} else if !strings.Contains(s.Detail, "Memgraph projection") {
			s.Detail += "; " + msg
		}
	}
	if s.Status == "ready" && s.MemgraphAvailable && !s.GraphStatsAvailable {
		s.Status = "missing"
		if fullCommand := codeGraphFullIndexSuggestionForStatus(*s); fullCommand != "" {
			s.SuggestedCommand = fullCommand
		}
		s.Detail = "Memgraph projection stats unavailable"
		if s.MemgraphDetail != "" {
			s.Detail += ": " + s.MemgraphDetail
		}
		if s.SuggestedCommand != "" {
			s.Detail += " - run gg doctor, then " + s.SuggestedCommand
		}
	}
	if s.Status == "ready" && s.MemgraphDetail != "not checked" && !s.MemgraphAvailable {
		s.Status = "missing"
		if fullCommand := codeGraphFullIndexSuggestionForStatus(*s); fullCommand != "" {
			s.SuggestedCommand = fullCommand
		}
		s.Detail = "Memgraph projection unavailable"
		if s.MemgraphDetail != "" {
			s.Detail += ": " + s.MemgraphDetail
		}
		if s.SuggestedCommand != "" {
			s.Detail += " - restore Memgraph, then run " + s.SuggestedCommand
		}
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
	fresh := s.freshnessContract()
	fmt.Fprintln(w, "CODE GRAPH:")
	fmt.Fprintf(w, "  Status: %s", fresh.Status)
	if fresh.Reason != "" {
		fmt.Fprintf(w, " (%s)", fresh.Reason)
	}
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
	if len(s.DetectedLanguages) > 0 {
		fmt.Fprintf(w, "  Detected: %s\n", strings.Join(s.DetectedLanguages, ", "))
	}
	if s.ChangedFiles+s.NewFiles+s.DeletedFiles+s.ModuleFiles > 0 {
		fmt.Fprintf(w, "  Changes: changed=%d new=%d deleted=%d modules=%d\n", s.ChangedFiles, s.NewFiles, s.DeletedFiles, s.ModuleFiles)
	}
	if fresh.SuggestedCommand != "" && fresh.NeedsNotice() {
		fmt.Fprintf(w, "  Suggested: %s\n", fresh.SuggestedCommand)
	}
	if fresh.ForegroundWatchAvailable && fresh.ForegroundWatchCommand != "" {
		fmt.Fprintf(w, "  Active mode: %s\n", fresh.ForegroundWatchCommand)
	}
	if fresh.Status != codeGraphFreshnessNotApplicable {
		fmt.Fprintf(w, "  Background refresh: %t\n", fresh.BackgroundRefresh)
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
