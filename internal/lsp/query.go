package lsp

import (
	"context"
	"fmt"
	"path/filepath"
)

// Kind is the LSP code-intelligence operation to run.
type Kind string

const (
	KindReferences Kind = "references"
	KindDefinition Kind = "definition"
	KindHover      Kind = "hover"
)

// Result carries the outcome of a single per-invocation query. Locations is
// populated for references/definition; Hover for hover. FileText is the opened
// source so the caller can map LSP 0-based positions back to 1-based display.
type Result struct {
	Kind      Kind
	Locations []Location
	Hover     HoverResult
	FileText  string
}

// Query runs one full per-invocation LSP exchange against the file's resolved
// language server: spawn → initialize → didOpen → the requested query →
// shutdown. line/col are 1-based (editor convention) and converted to LSP's
// 0-based UTF-16 position internally. rootDir is the project root used as the
// server's workspace; it defaults to the file's directory when empty.
//
// The whole exchange is bounded by ctx so a hung server cannot wedge the
// command. No server, goroutine, or process outlives this call.
func Query(ctx context.Context, kind Kind, file string, line, col int, rootDir string) (Result, error) {
	spec, err := ResolveServer(file)
	if err != nil {
		return Result{}, err
	}
	abs, err := filepath.Abs(file)
	if err != nil {
		return Result{}, fmt.Errorf("resolve %s: %w", file, err)
	}
	if rootDir == "" {
		rootDir = filepath.Dir(abs)
	}

	client, err := Dial(ctx, spec, rootDir)
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = client.Close(ctx) }()

	text, err := client.OpenFile(abs, spec.LanguageID)
	if err != nil {
		return Result{}, err
	}
	pos := HumanToLSP(text, line, col)
	uri := PathToURI(abs)
	res := Result{Kind: kind, FileText: text}

	switch kind {
	case KindReferences:
		locs, qErr := client.References(ctx, uri, pos)
		if qErr != nil {
			return Result{}, qErr
		}
		res.Locations = locs
	case KindDefinition:
		locs, qErr := client.Definition(ctx, uri, pos)
		if qErr != nil {
			return Result{}, qErr
		}
		res.Locations = locs
	case KindHover:
		hov, qErr := client.Hover(ctx, uri, pos)
		if qErr != nil {
			return Result{}, qErr
		}
		res.Hover = hov
	default:
		return Result{}, fmt.Errorf("unknown lsp query kind %q", kind)
	}
	return res, nil
}
