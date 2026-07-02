package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/gurkangul/gg-cli/internal/graph"
	"github.com/gurkangul/gg-cli/internal/index/parser"
	"github.com/gurkangul/gg-cli/internal/index/runner"
)

// index runs the SCIP indexer for a single module and processes the output into the graph.
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
		gc:               gc,
		root:             projectRoot,
		moduleDir:        moduleDir,
		moduleRelRoot:    moduleRelRoot,
		fileFilter:       fileFilter,
		headSHA:          headSHA,
		modulePath:       readModulePath(moduleDir),
		fileNodeByPath:   make(map[string]*graph.Node),
		scipToFile:       make(map[string]string),
		scipToSymbolID:   make(map[string]string),
		scipToSymbolName: make(map[string]string),
		seenImportEdges:  make(map[string]bool),
		seenRefEdges:     make(map[string]bool),
		seenPackages:     make(map[string]string),
	}
	if err := parser.ParseFile(ctx, result.IndexPath, string(lang), h); err != nil {
		return fmt.Errorf("parse scip: %w", err)
	}

	// BUG-013: resolve cross-file references and write IMPORTS edges now that
	// all definitions are in scipToFile.
	h.flushRefs(ctx)

	fmt.Printf("indexed %d files, %d symbols, %d references → %d import edges, %d reference edges, %d packages\n",
		h.files, h.symbols, h.refs, h.imports, h.references, len(h.seenPackages))
	return nil
}
