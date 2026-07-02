package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
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

var indexCmd = &cobra.Command{
	Use:   "index [--changed] [--lang go|python|swift|typescript]",
	Short: "Index the codebase into the embedded code graph (.gg/graph.db)",
	Long: `Runs a SCIP indexer on the project and writes the resulting code graph
(Symbol, File, Package nodes and DEFINES/IMPORTS edges) to the embedded SQLite
graph store (.gg/graph.db).
CALLS flow queries are supported when CALLS edges exist, but the built-in SCIP
parser currently materializes cross-file references as IMPORTS edges.

Without --changed: full re-index of the entire project.
With    --changed: incremental update — only files changed since the last
                   successful index are re-indexed (per CHANGED_CONTRACT.md).
With    --watch: explicit foreground watcher — debounce source/module changes
                   and run index updates until Ctrl-C. gg never starts a
                   background indexing daemon; use gg doctor --fix-index for
                   one-shot repair.`,
	RunE: runIndex,
}

var (
	indexChanged bool
	indexLang    string
	indexWatch   bool
)

func init() {
	indexCmd.Flags().BoolVar(&indexChanged, "changed", false, "incremental: re-index only files changed since last index")
	indexCmd.Flags().BoolVar(&indexWatch, "watch", false, "foreground watch mode: debounce source changes and run incremental index updates")
	indexCmd.Flags().StringVar(&indexLang, "lang", "go", "language to index: go, python, swift, typescript")
	rootCmd.AddCommand(indexCmd)
}

func runIndex(cmd *cobra.Command, _ []string) error {
	if indexWatch {
		return runIndexWatch(cmd)
	}
	// BUG-095: when --lang is not explicitly passed, refresh the language(s) the
	// project was already indexed as (recorded in index-state) instead of the
	// "go" flag default. The git hooks installed by `gg doctor --install-index-hooks`
	// run a language-agnostic `gg index --changed`; without this, that hook defaults
	// to go and silently fails ("no go modules found") on every non-go project, so
	// the CodeGraph never auto-refreshes for TS/Vue/Swift/Python repos. Explicit
	// --lang always wins; a never-indexed project still falls back to the go default.
	if !cmd.Flags().Changed("lang") {
		if langs := indexStateLanguages(); len(langs) > 0 {
			var firstErr error
			for _, l := range langs {
				if err := runIndexOnce(cmd, runner.Lang(l), indexChanged); err != nil && firstErr == nil {
					firstErr = err
				}
			}
			return firstErr
		}
	}
	return runIndexOnce(cmd, runner.Lang(indexLang), indexChanged)
}

// indexStateLanguages returns the languages recorded in this project's
// index-state (deterministic order), or nil when the root/state can't be read or
// nothing has been indexed yet. Lets a no-`--lang` `gg index` refresh exactly the
// languages already present in the project's graph instead of assuming go.
func indexStateLanguages() []string {
	root, err := config.FindRoot()
	if err != nil {
		return nil
	}
	s, err := state.Read(root + "/.gg")
	if err != nil {
		return nil
	}
	return s.IndexedLanguages()
}

func runIndexOnce(cmd *cobra.Command, lang runner.Lang, changedMode bool) error {
	cfg, err := config.Load()
	if err != nil {
		return configErr(fmt.Sprintf("load config: %v", err))
	}

	root, err := config.FindRoot()
	if err != nil {
		return configErr(err.Error())
	}

	ggDir := root + "/.gg"

	reg := runner.DefaultRegistry()
	r, ok := reg.Get(lang)
	if !ok {
		return fmt.Errorf("unsupported language %q — use %s", lang, strings.Join(langNames(runner.SupportedLangs()), ", "))
	}

	// Serialize graph writes. The detached git hooks fire-and-forget this, so a
	// quick commit→push can start a second run before the first finishes; skip
	// rather than race the graph DB. The running index covers the newer delta, or
	// the next git op re-fires the hook.
	release, acquired, lockErr := acquireIndexLock(ggDir, root, string(lang))
	if lockErr != nil {
		return fmt.Errorf("acquire index lock: %w", lockErr)
	}
	if !acquired {
		fmt.Println("gg index: another index run is active — skipping (graph refreshes on the next git op)")
		return nil
	}
	defer release()

	gc, err := graph.New(cfg.DataDir, cfg.ProjectID)
	if err != nil {
		return serviceErr(fmt.Sprintf("code graph client: %v", err))
	}
	defer func() { _ = gc.Close(cmd.Context()) }()

	ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Minute)
	defer cancel()

	// Ensure schema indexes exist. This is the first call that actually opens a
	// Bolt connection, so it is where a down/unreachable Memgraph surfaces.
	if err := gc.SchemaInit(ctx); err != nil {
		return memgraphDownErr("schema init", err)
	}

	if changedMode {
		return runChangedIndex(ctx, cmd, root, ggDir, lang, r, gc)
	}
	return runFullIndex(ctx, root, ggDir, lang, r, gc)
}

// indexOutboxPayload is the payload written to the outbox for index runs.
// It contains enough information for `gg doctor --reconcile` to re-run
// the index if the process died before state.json was written.
type indexOutboxPayload struct {
	Kind string `json:"kind"` // "full" or "changed"
	Root string `json:"root"`
	Lang string `json:"lang"`
	SHA  string `json:"sha"`
}

// runFullIndex runs a complete re-index of the project root.
func runFullIndex(ctx context.Context, root, ggDir string, lang runner.Lang, r runner.Runner, gc *graph.Client) error {
	moduleDirs, err := discoverModuleDirs(root, lang)
	if err != nil {
		return fmt.Errorf("discover %s modules: %w", lang, err)
	}
	if len(moduleDirs) == 0 {
		return fmt.Errorf("no %s modules found under %s — none of [%s] present at root or within doctor.hook_install.max_depth subdirs", lang, root, strings.Join(manifestsForLang(lang), ", "))
	}
	if err := runner.Preflight(lang); err != nil {
		return fmt.Errorf("indexer preflight (%s): %w", lang, err)
	}

	fmt.Printf("indexing %s (full, lang=%s, %d module(s)) ...\n", root, lang, len(moduleDirs))

	headSHA, err := changed.HeadSHA(ctx, root)
	if err != nil {
		return fmt.Errorf("get HEAD sha: %w", err)
	}

	// Record intent in the outbox before touching Memgraph.
	// If the process exits before state.json is written, reconcile will re-run.
	outboxID, outboxErr := outbox.Write(ggDir, "full-index", indexOutboxPayload{
		Kind: "full",
		Root: root,
		Lang: string(lang),
		SHA:  headSHA,
	})
	if outboxErr != nil {
		fmt.Fprintf(os.Stderr, "warning: outbox write failed (continuing): %v\n", outboxErr)
	}

	// Sweep existing nodes for this language before re-indexing. Language-scoped
	// sweeping lets multi-language projects accumulate Go/Python/TypeScript graph
	// slices without one full index deleting the others.
	fmt.Printf("sweeping stale %s graph nodes ...\n", lang)
	if sweepErr := gc.SweepProjectLang(ctx, string(lang)); sweepErr != nil {
		fmt.Fprintf(os.Stderr, "warning: sweep %s graph nodes failed (continuing): %v\n", lang, sweepErr)
	}

	for _, modDir := range moduleDirs {
		if err := index(ctx, root, modDir, lang, r, gc, nil, headSHA); err != nil {
			return err
		}
	}

	// Write state only on success.
	if err := writeIndexState(ctx, root, ggDir, headSHA, lang, langExtensions(lang)); err != nil {
		warnIndexStateWrite(err)
	} else {
		fmt.Printf("index-state.json updated (sha=%s)\n", headSHA[:8])
		// State is consistent — clear this outbox entry and any stale entries from
		// prior crashed runs for the same root+lang (a full reindex supersedes them).
		sweepIndexOutbox(ggDir, root, string(lang), outboxID)
	}
	return nil
}

// runChangedIndex runs an incremental re-index of files changed since the last index.
func runChangedIndex(ctx context.Context, cmd *cobra.Command, root, ggDir string, lang runner.Lang, r runner.Runner, gc *graph.Client) error {
	s, err := state.Read(ggDir)
	if errors.Is(err, state.ErrNoState) {
		fmt.Println("no previous index state — falling back to full index")
		return runFullIndex(ctx, root, ggDir, lang, r, gc)
	}
	if err != nil {
		return fmt.Errorf("read index state: %w", err)
	}

	langState, ok := s.ForLanguage(string(lang))
	if !ok {
		fmt.Printf("no previous %s index state — falling back to full index\n", lang)
		return runFullIndex(ctx, root, ggDir, lang, r, gc)
	}
	baseSHA := langState.LastIndexedSHA
	if baseSHA == changed.EmptyTreeSHA {
		fmt.Println("unborn git base detected — falling back to full index")
		return runFullIndex(ctx, root, ggDir, lang, r, gc)
	}

	// Verify that the last indexed SHA is a reachable ancestor of the current
	// HEAD. If it is not (branch switch, rebase, force push), the `git diff`
	// below would compute a wrong delta or fail entirely, and ghost symbols from
	// the old branch would survive in the graph. Fall back to a full re-index
	// which sweeps old nodes first.
	ancestor, ancestorErr := changed.IsAncestor(ctx, root, baseSHA)
	if ancestorErr != nil {
		fmt.Fprintf(os.Stderr, "warning: ancestor check failed (%v) — falling back to full index\n", ancestorErr)
		return runFullIndex(ctx, root, ggDir, lang, r, gc)
	}
	if !ancestor {
		fmt.Printf("non-linear history detected (last indexed sha=%s is not an ancestor of HEAD) — falling back to full index\n", indexStatusShortSHA(baseSHA))
		return runFullIndex(ctx, root, ggDir, lang, r, gc)
	}

	fmt.Printf("incremental index since %s ...\n", indexStatusShortSHA(baseSHA))

	// Compute changed files for the given language.
	exts := langExtensions(lang)
	headSHA, err := changed.HeadSHA(ctx, root)
	if err != nil {
		return fmt.Errorf("get HEAD sha: %w", err)
	}
	currentFingerprint, fpErr := changed.WorkingTreeFingerprintWithNames(ctx, root, baseSHA, exts, manifestsForLang(lang))
	if fpErr != nil {
		return fmt.Errorf("compute current source/module fingerprint: %w", fpErr)
	}
	if currentFingerprint == langState.WorkingTreeFingerprint {
		fmt.Println("indexed source/module fingerprint already matches current tree — advancing index-state")
		if err := writeIndexState(ctx, root, ggDir, headSHA, lang, langExtensions(lang)); err != nil {
			warnIndexStateWrite(err)
		} else {
			fmt.Printf("index-state.json updated (sha=%s)\n", headSHA[:8])
		}
		return nil
	}
	if langState.WorkingTreeFingerprint != "" {
		fmt.Println("indexed dirty source/module fingerprint changed — running full graph refresh to avoid stale dirty-tree projection")
		return runFullIndex(ctx, root, ggDir, lang, r, gc)
	}
	summary, summaryErr := codeGraphChangesSince(ctx, root, baseSHA, exts, manifestsForLang(lang))
	if summaryErr != nil {
		return fmt.Errorf("compute change summary: %w", summaryErr)
	}
	if summary.ModuleFiles > 0 {
		moduleDirs, discoverErr := discoverModuleDirs(root, lang)
		if discoverErr != nil {
			return fmt.Errorf("discover %s modules: %w", lang, discoverErr)
		}
		if len(moduleDirs) == 0 {
			fmt.Printf("module discovery changed and no %s module roots remain — sweeping stale %s graph nodes\n", lang, lang)
			if sweepErr := gc.SweepProjectLang(ctx, string(lang)); sweepErr != nil {
				return fmt.Errorf("sweep stale %s graph nodes after module removal: %w", lang, sweepErr)
			}
			if err := writeIndexState(ctx, root, ggDir, headSHA, lang, langExtensions(lang)); err != nil {
				warnIndexStateWrite(err)
			} else {
				fmt.Printf("index-state.json updated (sha=%s)\n", headSHA[:8])
			}
			return nil
		}
		fmt.Printf("module discovery changed (%d module file(s)) — running full %s graph refresh to avoid stale module projection\n", summary.ModuleFiles, lang)
		return runFullIndex(ctx, root, ggDir, lang, r, gc)
	}
	changedFiles, err := changed.Files(ctx, root, baseSHA, exts)
	if err != nil {
		return fmt.Errorf("compute changed files: %w", err)
	}

	if len(changedFiles) == 0 {
		fmt.Println("no changed files — index is up to date")
		if baseSHA != headSHA {
			if err := writeIndexState(ctx, root, ggDir, headSHA, lang, langExtensions(lang)); err != nil {
				warnIndexStateWrite(err)
			} else {
				fmt.Printf("index-state.json updated (sha=%s)\n", headSHA[:8])
			}
		}
		return nil
	}
	if summary.hasChanges() {
		fmt.Println(summary.detail("gg index --changed"))
	}
	if err := runner.Preflight(lang); err != nil {
		return fmt.Errorf("indexer preflight (%s): %w", lang, err)
	}

	changedRelFiles := make([]string, 0, len(changedFiles))
	for _, f := range changedFiles {
		rel, relErr := relForwardSlash(root, f)
		if relErr != nil {
			return fmt.Errorf("changed file %s outside project root: %w", f, relErr)
		}
		changedRelFiles = append(changedRelFiles, rel)
	}

	// Expand with 1-hop dependents (CHANGED_CONTRACT.md §2).
	toInvalidate := make(map[string]bool, len(changedRelFiles))
	for _, f := range changedRelFiles {
		toInvalidate[f] = true
	}
	for _, f := range changedRelFiles {
		deps, err := gc.DependentsOf(ctx, f)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: dependents of %s: %v\n", f, err)
			continue
		}
		for _, dep := range deps {
			toInvalidate[dep] = true
		}
	}

	fmt.Printf("invalidating %d file(s) ...\n", len(toInvalidate))
	for f := range toInvalidate {
		if err := gc.InvalidateFile(ctx, f); err != nil {
			return fmt.Errorf("invalidate %s: %w", f, err)
		}
	}

	// Re-run the SCIP indexer once for the full project.
	// The indexer doesn't support per-file mode, so we always index the whole
	// project and re-parse only the changed files from the resulting .scip output.
	// This is intentionally simple (see CHANGED_CONTRACT.md §6 — day-2: partial runs).
	// Record intent before Memgraph writes.
	outboxID, outboxErr := outbox.Write(ggDir, "changed-index", indexOutboxPayload{
		Kind: "changed",
		Root: root,
		Lang: string(lang),
		SHA:  headSHA,
	})
	if outboxErr != nil {
		fmt.Fprintf(os.Stderr, "warning: outbox write failed (continuing): %v\n", outboxErr)
	}

	moduleDirs, err := discoverModuleDirs(root, lang)
	if err != nil {
		return fmt.Errorf("discover %s modules: %w", lang, err)
	}
	if len(moduleDirs) == 0 {
		return fmt.Errorf("no %s modules found under %s — none of [%s] present at root or within doctor.hook_install.max_depth subdirs", lang, root, strings.Join(manifestsForLang(lang), ", "))
	}
	for _, modDir := range moduleDirs {
		if err := index(ctx, root, modDir, lang, r, gc, toInvalidate, headSHA); err != nil {
			return err
		}
	}

	if err := writeIndexState(ctx, root, ggDir, headSHA, lang, langExtensions(lang)); err != nil {
		warnIndexStateWrite(err)
	} else {
		fmt.Printf("index-state.json updated (sha=%s)\n", headSHA[:8])
		sweepIndexOutbox(ggDir, root, string(lang), outboxID)
	}
	return nil
}

// warnIndexStateWrite emits the single canonical stderr warning when the
// index-state.json write fails. Centralised so the wording stays consistent
// across the index flow's several write sites.
func warnIndexStateWrite(err error) {
	fmt.Fprintf(os.Stderr, "warning: could not write index-state.json: %v\n", err)
}

func writeIndexState(ctx context.Context, root, ggDir, headSHA string, lang runner.Lang, extensions []string) error {
	fingerprint, err := changed.WorkingTreeFingerprintWithNames(ctx, root, headSHA, extensions, manifestsForLang(lang))
	if err != nil {
		return err
	}
	return state.WriteLanguage(ggDir, string(lang), headSHA, fingerprint, extensions)
}

// sweepIndexOutbox deletes all pending outbox entries for the given root+lang
// (including currentID if non-empty). A completed index supersedes all prior
// incomplete attempts for the same project, so there is no reason to keep them.
func sweepIndexOutbox(ggDir, root, lang, currentID string) {
	if currentID != "" {
		if err := outbox.Delete(ggDir, currentID); err != nil {
			fmt.Fprintf(os.Stderr, "warning: outbox delete failed: %v\n", err)
		}
	}
	entries, err := outbox.List(ggDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: outbox list failed: %v\n", err)
		return
	}
	for _, e := range entries {
		if e.ID == currentID {
			continue
		}
		if e.Kind != "full-index" && e.Kind != "changed-index" {
			continue
		}
		var p indexOutboxPayload
		if jsonErr := json.Unmarshal(e.Payload, &p); jsonErr != nil {
			continue
		}
		// The outbox lives in the project-local .gg directory, so a completed
		// index for this language also supersedes stale entries written before a
		// project was moved/renamed. Legacy payloads may have an empty lang/root.
		// Keep entries for other languages: a TypeScript index must not hide a
		// still-pending Go/Python graph write.
		if p.Lang == lang || p.Lang == "" {
			if delErr := outbox.Delete(ggDir, e.ID); delErr != nil {
				fmt.Fprintf(os.Stderr, "warning: outbox delete failed (stale entry %s): %v\n", e.ID, delErr)
			}
		}
	}
}
