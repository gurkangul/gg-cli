package cmd

import (
	"context"
	"encoding/json"
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
	moduleDirs, err := discoverModuleDirs(root, lang)
	if err != nil {
		return fmt.Errorf("discover %s modules: %w", lang, err)
	}
	if len(moduleDirs) == 0 {
		return fmt.Errorf("no %s modules found under %s — %s not present at root or within doctor.hook_install.max_depth subdirs", lang, root, manifestForLang(lang))
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

	// Sweep all existing project nodes before re-indexing. Without this, nodes
	// from a previous branch (e.g. after a switch or rebase) survive as ghost
	// symbols — they exist in the graph but not in the current working tree.
	fmt.Println("sweeping stale graph nodes ...")
	if sweepErr := gc.SweepProject(ctx); sweepErr != nil {
		fmt.Fprintf(os.Stderr, "warning: sweep project failed (continuing): %v\n", sweepErr)
	}

	for _, modDir := range moduleDirs {
		if err := index(ctx, root, modDir, lang, r, gc, nil, headSHA); err != nil {
			return err
		}
	}

	// Write state only on success.
	if err := state.Write(ggDir, headSHA); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not write index-state.json: %v\n", err)
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

	moduleDirs, err := discoverModuleDirs(root, lang)
	if err != nil {
		return fmt.Errorf("discover %s modules: %w", lang, err)
	}
	if len(moduleDirs) == 0 {
		return fmt.Errorf("no %s modules found under %s — %s not present at root or within doctor.hook_install.max_depth subdirs", lang, root, manifestForLang(lang))
	}
	for _, modDir := range moduleDirs {
		if err := index(ctx, root, modDir, lang, r, gc, toInvalidate, headSHA); err != nil {
			return err
		}
	}

	if err := state.Write(ggDir, headSHA); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not write index-state.json: %v\n", err)
	} else {
		fmt.Printf("index-state.json updated (sha=%s)\n", headSHA[:8])
		sweepIndexOutbox(ggDir, root, string(lang), outboxID)
	}
	return nil
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
		if p.Root == root && p.Lang == lang {
			if delErr := outbox.Delete(ggDir, e.ID); delErr != nil {
				fmt.Fprintf(os.Stderr, "warning: outbox delete failed (stale entry %s): %v\n", e.ID, delErr)
			}
		}
	}
}

// index runs the SCIP indexer for a single module and processes the output into Memgraph.
// projectRoot is the repo/gg root used as the storage path origin.
// moduleDir is the directory scip-go runs in (where the language manifest — go.mod,
// package.json, pyproject.toml — lives). For single-module repos both are equal.
// fileFilter, when non-nil, limits processing to project-relative paths in the set.
// headSHA is stamped on every written node as indexed_at_commit.
func index(ctx context.Context, projectRoot, moduleDir string, lang runner.Lang, r runner.Runner, gc *graph.Client, fileFilter map[string]bool, headSHA string) error {
	moduleRelRoot, err := relForwardSlash(projectRoot, moduleDir)
	if err != nil {
		return fmt.Errorf("module dir %s outside project root %s: %w", moduleDir, projectRoot, err)
	}
	if moduleRelRoot != "." {
		fmt.Printf("→ module %s (scip cwd=%s)\n", moduleRelRoot, moduleDir)
	}

	req := &runner.IndexRequest{
		Root: moduleDir,
		Lang: lang,
	}
	result, err := r.Index(ctx, req)
	if err != nil {
		return fmt.Errorf("scip index (%s): %w", moduleRelRoot, err)
	}
	defer func() { _ = os.Remove(result.IndexPath) }() // temp file cleanup

	if len(result.Stderr) > 0 {
		fmt.Fprintf(os.Stderr, "indexer: %s\n", result.Stderr)
	}

	fmt.Printf("parsing %s ...\n", result.IndexPath)

	h := &graphHandler{
		gc:              gc,
		root:            projectRoot,
		moduleDir:       moduleDir,
		moduleRelRoot:   moduleRelRoot,
		fileFilter:      fileFilter,
		headSHA:         headSHA,
		modulePath:      readModulePath(moduleDir),
		fileNodeByPath:  make(map[string]*graph.Node),
		scipToFile:      make(map[string]string),
		seenImportEdges: make(map[string]bool),
		seenPackages:    make(map[string]string),
	}
	if err := parser.ParseFile(ctx, result.IndexPath, string(lang), h); err != nil {
		return fmt.Errorf("parse scip: %w", err)
	}

	// BUG-013: resolve cross-file references and write IMPORTS edges now that
	// all definitions are in scipToFile.
	h.flushRefs(ctx)

	fmt.Printf("indexed %d files, %d symbols, %d references → %d import edges, %d packages\n",
		h.files, h.symbols, h.refs, h.imports, len(h.seenPackages))
	return nil
}

// readModulePath reads the Go module path from go.mod in the project root.
// Returns "" if go.mod is absent or the module line is not found.
func readModulePath(root string) string {
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return ""
	}
	for _, line := range strings.SplitN(string(data), "\n", 20) {
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module "))
		}
	}
	return ""
}

// graphHandler implements parser.Handler, writing parsed nodes to Memgraph.
type graphHandler struct {
	gc               *graph.Client
	root             string          // absolute project/gg root — all stored paths are relative to this
	moduleDir        string          // absolute dir where scip-go ran (holds go.mod / package.json)
	moduleRelRoot    string          // moduleDir relative to project root, forward slashes ("." for root-level module)
	headSHA          string          // stamped on every node as indexed_at_commit
	fileFilter       map[string]bool // if non-nil, only these project-relative paths are written
	modulePath       string          // go module path read from go.mod (used for Package import_path)
	files            int
	symbols          int
	refs             int
	imports          int
	skippedOutOfTree int // files skipped because their path escaped project root (e.g. go-build cache)

	// BUG-013/014: per-run lookup tables built during indexing.
	fileNodeByPath  map[string]*graph.Node // relPath → file node (with ID)
	scipToFile      map[string]string      // scip symbol string → source relPath (for ref resolution)
	seenImportEdges map[string]bool        // "fromID|toID" → written (dedup)
	seenPackages    map[string]string      // import_path → package node ID
	pendingRefs     []pendingRef
}

// pendingRef is a cross-file reference collected during OnReference and resolved after ParseFile.
type pendingRef struct {
	fromFileID string
	scipSymbol string
}

func (h *graphHandler) OnFile(ctx context.Context, node *graph.Node) error {
	rawPath, _ := node.Properties["path"].(string)
	relPath, ok := normalizeProjectPath(h.root, h.moduleDir, rawPath)
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
	h.fileNodeByPath[relPath] = node
	h.files++

	// BUG-014: create Package node and CONTAINS edge for this file.
	// Use the module-relative path so the derived Go import path does not include
	// the repo-relative module prefix (e.g. lift-cli/cmd → cmd under module root).
	lang, _ := node.Properties["lang"].(string)
	moduleRelPath := stripModulePrefix(relPath, h.moduleRelRoot)
	pkgImportPath := derivePackagePath(h.modulePath, lang, moduleRelPath)
	if pkgImportPath != "" {
		pkgNode, err := h.upsertPackage(ctx, lang, pkgImportPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: upsert package %s: %v\n", pkgImportPath, err)
		} else if pkgNode != nil && pkgNode.ID != "" && node.ID != "" {
			edge := &graph.Edge{FromID: pkgNode.ID, ToID: node.ID, Type: graph.RelContains}
			if err := h.gc.UpsertEdge(ctx, edge); err != nil {
				fmt.Fprintf(os.Stderr, "warning: upsert CONTAINS edge: %v\n", err)
			}
		}
	}
	return nil
}

func (h *graphHandler) OnSymbol(ctx context.Context, fileNode *graph.Node, symNode *graph.Node, scipSymbol string) error {
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
	// BUG-013: record scip→file mapping for cross-file reference resolution.
	if scipSymbol != "" {
		h.scipToFile[scipSymbol] = relPath
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
	// BUG-013: collect pending cross-file refs; flushed after ParseFile completes
	// so all definitions are guaranteed to be in scipToFile before resolution.
	if fromFileNode.ID != "" && scipSymbol != "" {
		h.pendingRefs = append(h.pendingRefs, pendingRef{
			fromFileID: fromFileNode.ID,
			scipSymbol: scipSymbol,
		})
	}
	return nil
}

// flushRefs resolves collected pending references and writes IMPORTS edges.
// Must be called after ParseFile — at that point all definitions are in scipToFile.
func (h *graphHandler) flushRefs(ctx context.Context) {
	for _, ref := range h.pendingRefs {
		targetFile := h.scipToFile[ref.scipSymbol]
		if targetFile == "" {
			continue // external/stdlib symbol — no node in this project's graph
		}
		targetNode := h.fileNodeByPath[targetFile]
		if targetNode == nil || targetNode.ID == "" || targetNode.ID == ref.fromFileID {
			continue // target not indexed or self-reference
		}
		edgeKey := ref.fromFileID + "|" + targetNode.ID
		if h.seenImportEdges[edgeKey] {
			continue
		}
		h.seenImportEdges[edgeKey] = true
		edge := &graph.Edge{FromID: ref.fromFileID, ToID: targetNode.ID, Type: graph.RelImports}
		if err := h.gc.UpsertEdge(ctx, edge); err != nil {
			fmt.Fprintf(os.Stderr, "warning: upsert IMPORTS edge: %v\n", err)
		} else {
			h.imports++
		}
	}
}

// upsertPackage creates or retrieves a Package node for the given import path.
// Returns the node (with ID set) on success, nil if already seen this run.
func (h *graphHandler) upsertPackage(ctx context.Context, lang, importPath string) (*graph.Node, error) {
	if id, ok := h.seenPackages[importPath]; ok {
		return &graph.Node{ID: id, Label: graph.LabelPackage}, nil
	}
	pkgName := importPath
	if idx := strings.LastIndex(importPath, "/"); idx >= 0 {
		pkgName = importPath[idx+1:]
	}
	node := graph.PackageNode(pkgName, lang, importPath)
	if err := h.gc.UpsertNode(ctx, node, []string{"import_path"}); err != nil {
		return nil, err
	}
	h.seenPackages[importPath] = node.ID
	return node, nil
}

// derivePackagePath returns the canonical package import path for a source file.
// For Go, it combines the module path with the file's directory segment.
func derivePackagePath(modulePath, lang, relFilePath string) string {
	dir := ""
	if idx := strings.LastIndex(relFilePath, "/"); idx >= 0 {
		dir = relFilePath[:idx]
	}
	if lang == "go" {
		if modulePath == "" {
			return dir
		}
		if dir == "" {
			return modulePath // root package
		}
		return modulePath + "/" + dir
	}
	// TypeScript / Python: use directory path as package identifier.
	if dir == "" {
		return "."
	}
	return dir
}

// normalizeProjectPath accepts whatever the SCIP parser emitted for a file and
// returns a cleaned project-relative path. Relative inputs are interpreted
// against baseDir (scip-go's cwd — the module dir, which may be a subdirectory
// of the project root in monorepos). Returns ok=false when the path escapes
// the project root (absolute paths outside root, or rel paths that resolve
// via "..") — callers should skip such files.
func normalizeProjectPath(projectRoot, baseDir, raw string) (string, bool) {
	if raw == "" {
		return "", false
	}
	absRoot, err := filepath.Abs(projectRoot)
	if err != nil {
		return "", false
	}
	base := absRoot
	if baseDir != "" {
		absBase, baseErr := filepath.Abs(baseDir)
		if baseErr != nil {
			return "", false
		}
		base = absBase
	}
	var candidate string
	if filepath.IsAbs(raw) {
		candidate = filepath.Clean(raw)
	} else {
		candidate = filepath.Clean(filepath.Join(base, raw))
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

// relForwardSlash returns target relative to base using forward slashes.
// Returns "." when the paths are equivalent, or an error if target escapes base.
func relForwardSlash(base, target string) (string, error) {
	absBase, err := filepath.Abs(base)
	if err != nil {
		return "", err
	}
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(absBase, absTarget)
	if err != nil {
		return "", err
	}
	if strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("%s escapes %s", target, base)
	}
	return filepath.ToSlash(rel), nil
}

// stripModulePrefix returns projectRelPath with the moduleRelRoot prefix
// removed, producing a module-relative path. "." (root-level module) is a
// no-op. Paths not under moduleRelRoot are returned unchanged (callers may
// feed out-of-module files when multiple modules share a parser pass).
func stripModulePrefix(projectRelPath, moduleRelRoot string) string {
	if moduleRelRoot == "" || moduleRelRoot == "." {
		return projectRelPath
	}
	if after, ok := strings.CutPrefix(projectRelPath, moduleRelRoot+"/"); ok {
		return after
	}
	return projectRelPath
}

// manifestForLang returns the canonical manifest filename whose presence marks
// a module/package root for the given language. Returns "" for unsupported langs.
func manifestForLang(lang runner.Lang) string {
	switch lang {
	case runner.LangGo:
		return "go.mod"
	case runner.LangTypeScript:
		return "package.json"
	case runner.LangPython:
		return "pyproject.toml"
	}
	return ""
}

// discoverModuleDirs walks the project root looking for language manifest
// files (go.mod, package.json, pyproject.toml) and returns the absolute
// directories that contain them. A manifest at the project root short-circuits
// the walk and returns [projectRoot] — preserving single-module behaviour.
// Walk depth and skip-directory list are shared with `gg doctor
// --install-task-hooks` so monorepo heuristics stay consistent across commands.
func discoverModuleDirs(projectRoot string, lang runner.Lang) ([]string, error) {
	manifest := manifestForLang(lang)
	if manifest == "" {
		return nil, fmt.Errorf("unsupported language %q", lang)
	}
	// Fast path: manifest at root → single-module project.
	if _, err := os.Stat(filepath.Join(projectRoot, manifest)); err == nil {
		return []string{projectRoot}, nil
	}
	skipDirs, maxDepth := hookInstallSettings()
	relDirs, err := findManifestDirs(projectRoot, manifest, skipDirs, maxDepth)
	if err != nil {
		return nil, err
	}
	absDirs := make([]string, 0, len(relDirs))
	for _, rel := range relDirs {
		if rel == "." {
			absDirs = append(absDirs, projectRoot)
			continue
		}
		absDirs = append(absDirs, filepath.Join(projectRoot, rel))
	}
	return absDirs, nil
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
