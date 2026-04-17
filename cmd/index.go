package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/graph"
	"github.com/gurkangul/gg-cli/internal/index/changed"
	"github.com/gurkangul/gg-cli/internal/index/parser"
	"github.com/gurkangul/gg-cli/internal/index/runner"
	"github.com/gurkangul/gg-cli/internal/index/state"
	"github.com/gurkangul/gg-cli/internal/outbox"
)

var indexCmd = &cobra.Command{
	Use:   "index [--changed] [--lang go|python|typescript]",
	Short: "Index the codebase into the Memgraph knowledge graph",
	Long: `Runs a SCIP indexer on the project and writes the resulting code graph
(Symbol, File, Package nodes and DEFINES/IMPORTS/CALLS edges) to Memgraph.

Without --changed: full re-index of the entire project.
With    --changed: incremental update — only files changed since the last
                   successful index are re-indexed (per CHANGED_CONTRACT.md).`,
	RunE: runIndex,
}

var (
	indexChanged bool
	indexLang    string
)

func init() {
	indexCmd.Flags().BoolVar(&indexChanged, "changed", false, "incremental: re-index only files changed since last index")
	indexCmd.Flags().StringVar(&indexLang, "lang", "go", "language to index: go, python, typescript")
	rootCmd.AddCommand(indexCmd)
}

func runIndex(cmd *cobra.Command, _ []string) error {
	cfg, err := config.Load()
	if err != nil {
		return configErr(fmt.Sprintf("load config: %v", err))
	}

	root, err := config.FindRoot()
	if err != nil {
		return configErr(err.Error())
	}

	ggDir := root + "/.gg"

	lang := runner.Lang(indexLang)
	reg := runner.DefaultRegistry()
	r, ok := reg.Get(lang)
	if !ok {
		return fmt.Errorf("unsupported language %q — use go, python, or typescript", indexLang)
	}

	gc, err := graph.New(&cfg.Memgraph, cfg.ProjectID)
	if err != nil {
		return serviceErr(fmt.Sprintf("memgraph client: %v", err))
	}
	defer func() { _ = gc.Close(cmd.Context()) }()

	ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Minute)
	defer cancel()

	// Ensure schema indexes exist.
	if err := gc.SchemaInit(ctx); err != nil {
		return fmt.Errorf("schema init: %w", err)
	}

	if indexChanged {
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
	fmt.Printf("indexing %s (full, lang=%s) ...\n", root, lang)

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

	// Sweep all existing project nodes before re-indexing. Without this, nodes
	// from a previous branch (e.g. after a switch or rebase) survive as ghost
	// symbols — they exist in the graph but not in the current working tree.
	fmt.Println("sweeping stale graph nodes ...")
	if sweepErr := gc.SweepProject(ctx); sweepErr != nil {
		fmt.Fprintf(os.Stderr, "warning: sweep project failed (continuing): %v\n", sweepErr)
	}

	if err := index(ctx, root, lang, r, gc, nil, headSHA); err != nil {
		return err
	}

	// Write state only on success.
	if err := state.Write(ggDir, headSHA); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not write index-state.json: %v\n", err)
	} else {
		fmt.Printf("index-state.json updated (sha=%s)\n", headSHA[:8])
		// State is consistent — clear the outbox entry.
		if outboxID != "" {
			if delErr := outbox.Delete(ggDir, outboxID); delErr != nil {
				fmt.Fprintf(os.Stderr, "warning: outbox delete failed: %v\n", delErr)
			}
		}
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

	// Verify that the last indexed SHA is a reachable ancestor of the current
	// HEAD. If it is not (branch switch, rebase, force push), the `git diff`
	// below would compute a wrong delta or fail entirely, and ghost symbols from
	// the old branch would survive in the graph. Fall back to a full re-index
	// which sweeps old nodes first.
	ancestor, ancestorErr := changed.IsAncestor(ctx, root, s.LastIndexedSHA)
	if ancestorErr != nil {
		fmt.Fprintf(os.Stderr, "warning: ancestor check failed (%v) — falling back to full index\n", ancestorErr)
		return runFullIndex(ctx, root, ggDir, lang, r, gc)
	}
	if !ancestor {
		fmt.Printf("non-linear history detected (last indexed sha=%s is not an ancestor of HEAD) — falling back to full index\n", s.LastIndexedSHA[:8])
		return runFullIndex(ctx, root, ggDir, lang, r, gc)
	}

	fmt.Printf("incremental index since %s ...\n", s.LastIndexedSHA[:8])

	// Compute changed files for the given language.
	exts := langExtensions(lang)
	changedFiles, err := changed.Files(ctx, root, s.LastIndexedSHA, exts)
	if err != nil {
		return fmt.Errorf("compute changed files: %w", err)
	}

	if len(changedFiles) == 0 {
		fmt.Println("no changed files — index is up to date")
		return nil
	}

	// Expand with 1-hop dependents (CHANGED_CONTRACT.md §2).
	toInvalidate := make(map[string]bool, len(changedFiles))
	for _, f := range changedFiles {
		toInvalidate[f] = true
	}
	for _, f := range changedFiles {
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
	headSHA, err := changed.HeadSHA(ctx, root)
	if err != nil {
		return fmt.Errorf("get HEAD sha: %w", err)
	}

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

	if err := index(ctx, root, lang, r, gc, toInvalidate, headSHA); err != nil {
		return err
	}

	if err := state.Write(ggDir, headSHA); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not write index-state.json: %v\n", err)
	} else {
		fmt.Printf("index-state.json updated (sha=%s)\n", headSHA[:8])
		if outboxID != "" {
			if delErr := outbox.Delete(ggDir, outboxID); delErr != nil {
				fmt.Fprintf(os.Stderr, "warning: outbox delete failed: %v\n", delErr)
			}
		}
	}
	return nil
}

// index runs the SCIP indexer and processes the output into Memgraph.
// If fileFilter is non-nil, only documents whose path is in the filter are processed.
// headSHA is stamped on every written node as indexed_at_commit.
func index(ctx context.Context, root string, lang runner.Lang, r runner.Runner, gc *graph.Client, fileFilter map[string]bool, headSHA string) error {
	req := &runner.IndexRequest{
		Root: root,
		Lang: lang,
	}
	result, err := r.Index(ctx, req)
	if err != nil {
		return fmt.Errorf("scip index: %w", err)
	}
	defer func() { _ = os.Remove(result.IndexPath) }() // temp file cleanup

	if len(result.Stderr) > 0 {
		fmt.Fprintf(os.Stderr, "indexer: %s\n", result.Stderr)
	}

	fmt.Printf("parsing %s ...\n", result.IndexPath)

	h := &graphHandler{gc: gc, root: root, fileFilter: fileFilter, headSHA: headSHA}
	if err := parser.ParseFile(ctx, result.IndexPath, string(lang), h); err != nil {
		return fmt.Errorf("parse scip: %w", err)
	}

	fmt.Printf("indexed %d files, %d symbols, %d references\n", h.files, h.symbols, h.refs)
	return nil
}

// graphHandler implements parser.Handler, writing parsed nodes to Memgraph.
type graphHandler struct {
	gc                *graph.Client
	root              string
	headSHA           string          // stamped on every node as indexed_at_commit
	fileFilter        map[string]bool // if non-nil, only these relative paths are written
	files             int
	symbols           int
	refs              int
	skippedOutOfTree  int // files skipped because their path escaped project root (e.g. go-build cache)
}

func (h *graphHandler) OnFile(ctx context.Context, node *graph.Node) error {
	rawPath, _ := node.Properties["path"].(string)
	relPath, ok := normalizeProjectPath(h.root, rawPath)
	if !ok {
		// Path escapes project root (e.g. Go build-cache artefact like
		// "../../Library/Caches/go-build/..") or is otherwise outside the tree.
		// Skip silently — these aren't part of the project's source graph.
		h.skippedOutOfTree++
		return nil
	}
	if h.fileFilter != nil && !h.fileFilter[relPath] {
		return nil
	}
	// Store project-relative paths for cross-machine portability (BUG-010 fix).
	// CHANGED_CONTRACT.md §5 requires source_file on every node for reaping;
	// relative form keeps the contract while making brain export git-safe.
	node.Properties["path"] = relPath
	node.Properties["source_file"] = relPath
	if h.headSHA != "" {
		node.Properties["indexed_at_commit"] = h.headSHA
	}
	if err := h.gc.UpsertNode(ctx, node, []string{"path"}); err != nil {
		return err
	}
	h.files++
	return nil
}

func (h *graphHandler) OnSymbol(ctx context.Context, fileNode *graph.Node, symNode *graph.Node) error {
	// fileNode.Properties["path"] was normalised to relative by OnFile.
	relPath, _ := fileNode.Properties["path"].(string)
	if relPath == "" {
		// File was skipped (out-of-tree); skip its symbols too.
		return nil
	}
	if h.fileFilter != nil && !h.fileFilter[relPath] {
		return nil
	}
	symNode.Properties["source_file"] = relPath
	if h.headSHA != "" {
		symNode.Properties["indexed_at_commit"] = h.headSHA
	}
	if err := h.gc.UpsertNode(ctx, symNode, []string{"name", "source_file"}); err != nil {
		return err
	}
	if fileNode.ID != "" && symNode.ID != "" {
		edge := &graph.Edge{
			FromID: fileNode.ID,
			ToID:   symNode.ID,
			Type:   graph.RelDefines,
		}
		if err := h.gc.UpsertEdge(ctx, edge); err != nil {
			fmt.Fprintf(os.Stderr, "warning: upsert DEFINES edge: %v\n", err)
		}
	}
	h.symbols++
	return nil
}

func (h *graphHandler) OnReference(ctx context.Context, fromFileNode *graph.Node, scipSymbol string) error {
	h.refs++
	// Cross-file reference edges are day-2 — requires symbol-to-node lookup.
	// Counted here for metrics but not persisted yet.
	_ = scipSymbol
	return nil
}

// normalizeProjectPath accepts whatever the SCIP parser emitted for a file and
// returns a cleaned project-relative path. Returns ok=false when the path
// escapes the project root (absolute paths outside root, or rel paths that
// resolve via "..") — callers should skip such files.
func normalizeProjectPath(root, raw string) (string, bool) {
	if raw == "" {
		return "", false
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", false
	}
	var candidate string
	if filepath.IsAbs(raw) {
		candidate = filepath.Clean(raw)
	} else {
		candidate = filepath.Clean(filepath.Join(absRoot, raw))
	}
	rel, err := filepath.Rel(absRoot, candidate)
	if err != nil {
		return "", false
	}
	// filepath.Rel can return "../.." when candidate escapes root.
	if rel == "." || rel == "" {
		return "", false
	}
	if strings.HasPrefix(rel, "..") {
		return "", false
	}
	// Always use forward-slash paths — portable across OS.
	return filepath.ToSlash(rel), true
}

// langExtensions maps a Lang to the file extensions its indexer handles.
func langExtensions(lang runner.Lang) []string {
	switch lang {
	case runner.LangGo:
		return []string{".go"}
	case runner.LangPython:
		return []string{".py"}
	case runner.LangTypeScript:
		return []string{".ts", ".tsx", ".js", ".jsx"}
	default:
		return nil
	}
}
