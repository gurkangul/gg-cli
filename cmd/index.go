package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg/internal/config"
	"github.com/gurkangul/gg/internal/graph"
	"github.com/gurkangul/gg/internal/index/changed"
	"github.com/gurkangul/gg/internal/index/parser"
	"github.com/gurkangul/gg/internal/index/runner"
	"github.com/gurkangul/gg/internal/index/state"
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
	defer gc.Close(cmd.Context())

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

// runFullIndex runs a complete re-index of the project root.
func runFullIndex(ctx context.Context, root, ggDir string, lang runner.Lang, r runner.Runner, gc *graph.Client) error {
	fmt.Printf("indexing %s (full, lang=%s) ...\n", root, lang)

	headSHA, err := changed.HeadSHA(ctx, root)
	if err != nil {
		return fmt.Errorf("get HEAD sha: %w", err)
	}

	if err := index(ctx, root, lang, r, gc, nil); err != nil {
		return err
	}

	// Write state only on success.
	if err := state.Write(ggDir, headSHA); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not write index-state.json: %v\n", err)
	} else {
		fmt.Printf("index-state.json updated (sha=%s)\n", headSHA[:8])
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
	if err := index(ctx, root, lang, r, gc, toInvalidate); err != nil {
		return err
	}

	headSHA, err := changed.HeadSHA(ctx, root)
	if err != nil {
		return fmt.Errorf("get HEAD sha: %w", err)
	}

	if err := state.Write(ggDir, headSHA); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not write index-state.json: %v\n", err)
	} else {
		fmt.Printf("index-state.json updated (sha=%s)\n", headSHA[:8])
	}
	return nil
}

// index runs the SCIP indexer and processes the output into Memgraph.
// If fileFilter is non-nil, only documents whose path is in the filter are processed.
func index(ctx context.Context, root string, lang runner.Lang, r runner.Runner, gc *graph.Client, fileFilter map[string]bool) error {
	req := &runner.IndexRequest{
		Root: root,
		Lang: lang,
	}
	result, err := r.Index(ctx, req)
	if err != nil {
		return fmt.Errorf("scip index: %w", err)
	}
	defer os.Remove(result.IndexPath) // temp file cleanup

	if len(result.Stderr) > 0 {
		fmt.Fprintf(os.Stderr, "indexer: %s\n", result.Stderr)
	}

	fmt.Printf("parsing %s ...\n", result.IndexPath)

	h := &graphHandler{gc: gc, root: root, fileFilter: fileFilter}
	if err := parser.ParseFile(ctx, result.IndexPath, string(lang), h); err != nil {
		return fmt.Errorf("parse scip: %w", err)
	}

	fmt.Printf("indexed %d files, %d symbols, %d references\n", h.files, h.symbols, h.refs)
	return nil
}

// graphHandler implements parser.Handler, writing parsed nodes to Memgraph.
type graphHandler struct {
	gc         *graph.Client
	root       string
	fileFilter map[string]bool // if non-nil, only these file paths are written
	files      int
	symbols    int
	refs       int
}

func (h *graphHandler) OnFile(ctx context.Context, node *graph.Node) error {
	relPath, _ := node.Properties["path"].(string)
	absPath := absFilePath(h.root, relPath)
	if h.fileFilter != nil && !h.fileFilter[absPath] {
		return nil
	}
	// Overwrite path with absolute form and set source_file for idempotent reaping.
	node.Properties["path"] = absPath
	node.Properties["source_file"] = absPath
	if err := h.gc.CreateNode(ctx, node); err != nil {
		return err
	}
	h.files++
	return nil
}

func (h *graphHandler) OnSymbol(ctx context.Context, fileNode *graph.Node, symNode *graph.Node) error {
	// fileNode.Properties["path"] was already set to absolute by OnFile.
	absPath, _ := fileNode.Properties["path"].(string)
	if absPath == "" {
		absPath = absFilePath(h.root, "")
	}
	if h.fileFilter != nil && !h.fileFilter[absPath] {
		return nil
	}
	// source_file is mandatory (CHANGED_CONTRACT.md §5) so reaping can find this node.
	symNode.Properties["source_file"] = absPath
	if err := h.gc.CreateNode(ctx, symNode); err != nil {
		return err
	}
	// Create DEFINES edge: (File)-[:DEFINES]->(Symbol)
	if fileNode.ID != "" && symNode.ID != "" {
		edge := &graph.Edge{
			FromID: fileNode.ID,
			ToID:   symNode.ID,
			Type:   graph.RelDefines,
		}
		if err := h.gc.CreateEdge(ctx, edge); err != nil {
			// Non-fatal: log and continue.
			fmt.Fprintf(os.Stderr, "warning: create DEFINES edge: %v\n", err)
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

// absFilePath converts a project-relative path to absolute using the project root.
func absFilePath(root, rel string) string {
	if rel == "" {
		return root
	}
	return root + "/" + rel
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
