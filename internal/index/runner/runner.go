// Package runner executes SCIP-based indexers and returns the path to the
// produced .scip index file. Each language has its own Runner implementation
// that wraps the appropriate SCIP CLI binary (scip-go, scip-python,
// scip-typescript). Binaries are resolved via the chain in resolve.go.
package runner

import (
	"context"
	"fmt"
)

// IndexRequest describes a single indexing run.
type IndexRequest struct {
	// Root is the absolute path to the project directory to index.
	Root string
	// Lang selects which indexer to use.
	Lang Lang
	// OutputPath is the absolute path where the .scip file should be written.
	// If empty, the runner writes to a temp file and sets Result.IndexPath.
	OutputPath string
}

// IndexResult holds the output of a successful indexing run.
type IndexResult struct {
	// IndexPath is the absolute path to the produced .scip file.
	IndexPath string
	// Stderr captures indexer diagnostic output (warnings, progress).
	Stderr []byte
}

// Lang identifies the programming language to index.
type Lang string

const (
	LangGo         Lang = "go"
	LangPython     Lang = "python"
	LangSwift      Lang = "swift"
	LangTypeScript Lang = "typescript"
)

// SupportedLangs returns every language exposed by gg index in stable display
// order. Keep command validation, freshness detection, and docs wired through
// this helper to avoid one command accepting a language another command cannot
// reason about.
func SupportedLangs() []Lang {
	return []Lang{LangGo, LangPython, LangSwift, LangTypeScript}
}

// IndexerBinary returns the executable name for a language runner.
func IndexerBinary(lang Lang) (string, bool) {
	switch lang {
	case LangGo:
		return "scip-go", true
	case LangPython:
		return "scip-python", true
	case LangSwift:
		return "scip-swift", true
	case LangTypeScript:
		return "scip-typescript", true
	default:
		return "", false
	}
}

// Preflight verifies that the indexer binary for lang is available before any
// caller mutates derived graph state. Individual runners resolve again when
// executing so PATH changes between preflight and run are still respected.
func Preflight(lang Lang) error {
	bin, ok := IndexerBinary(lang)
	if !ok {
		return fmt.Errorf("unsupported language %q", lang)
	}
	_, err := ResolveIndexer(bin)
	return err
}

// Runner is the interface every language indexer must satisfy.
type Runner interface {
	// Lang returns the language this runner handles.
	Lang() Lang
	// Index runs the SCIP indexer for the given request.
	// It blocks until the indexer exits. Context cancellation terminates the child process.
	Index(ctx context.Context, req *IndexRequest) (*IndexResult, error)
}

// Registry holds a set of runners keyed by language.
type Registry struct {
	runners map[Lang]Runner
}

// NewRegistry creates an empty registry. Use Register to add runners.
func NewRegistry() *Registry {
	return &Registry{runners: make(map[Lang]Runner)}
}

// Register adds a runner to the registry. Panics on duplicate Lang.
func (r *Registry) Register(runner Runner) {
	l := runner.Lang()
	if _, exists := r.runners[l]; exists {
		panic(fmt.Sprintf("runner already registered for lang %q", l))
	}
	r.runners[l] = runner
}

// Get returns the runner for the given language.
func (r *Registry) Get(lang Lang) (Runner, bool) {
	runner, ok := r.runners[lang]
	return runner, ok
}

// DefaultRegistry builds a registry with all built-in language runners.
// Binary resolution is performed lazily on the first Index call.
func DefaultRegistry() *Registry {
	reg := NewRegistry()
	reg.Register(&GoRunner{})
	reg.Register(&PythonRunner{})
	reg.Register(&SwiftRunner{})
	reg.Register(&TypeScriptRunner{})
	return reg
}
