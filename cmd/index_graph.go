package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gurkangul/gg-cli/internal/graph"
)

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
	// Store project-relative paths for cross-machine portability (BUG-010 fix).
	// Set this before the incremental filter so OnSymbol sees the same key for
	// filtered-out target files in nested modules.
	node.Properties["path"] = relPath
	node.Properties["source_file"] = relPath
	if h.fileFilter != nil && !h.fileFilter[relPath] {
		// Incremental runs still parse the full SCIP output. For files outside the
		// write filter, keep enough existing graph identity to resolve imports from
		// changed files to unchanged target files without rewriting the target.
		existing, err := h.gc.FindNodeByProperty(ctx, graph.LabelFile, "path", relPath)
		if err != nil {
			return err
		}
		if existing != nil {
			h.fileNodeByPath[relPath] = existing
		}
		return nil
	}
	// CHANGED_CONTRACT.md §5 requires source_file on every node for reaping;
	// relative form keeps the contract while making brain export git-safe.
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
		if scipSymbol != "" {
			h.scipToFile[scipSymbol] = relPath
		}
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
