// Package parser converts a SCIP index into graph nodes and edges.
//
// Mapping rules (Day-1 scope, aligned with TASK-007 schema):
//
//   SCIP Occurrence with SymbolRole_Definition set  →  Symbol node (graph.SymbolNode)
//   SCIP Occurrence without Definition role          →  CALLS/REFERENCES edge from referencing file's Definition
//   SCIP Document                                    →  File node (graph.FileNode)
//
// The parser is streaming: it visits documents one at a time so the caller
// can write to Memgraph incrementally without loading the entire index into memory.
package parser

import (
	"context"
	"fmt"
	"os"

	scippb "github.com/scip-code/scip/bindings/go/scip"

	"github.com/gurkangul/gg/internal/graph"
)

// Handler receives parsed graph primitives. All methods are called from a
// single goroutine (the parse goroutine) in document order.
type Handler interface {
	// OnFile is called once per SCIP Document before its symbols are processed.
	OnFile(ctx context.Context, node *graph.Node) error
	// OnSymbol is called for every Definition occurrence within a document.
	OnSymbol(ctx context.Context, fileNode *graph.Node, symNode *graph.Node) error
	// OnReference is called for every non-Definition occurrence that refers to
	// a known symbol. fromFileNode is the file containing the reference.
	OnReference(ctx context.Context, fromFileNode *graph.Node, scipSymbol string) error
}

// ParseFile reads a .scip file from disk and calls handler methods as it
// encounters definitions and references.
func ParseFile(ctx context.Context, scipPath string, lang string, h Handler) error {
	f, err := os.Open(scipPath)
	if err != nil {
		return fmt.Errorf("open scip index %s: %w", scipPath, err)
	}
	defer f.Close()

	visitor := &scippb.IndexVisitor{
		VisitDocument: func(ctx context.Context, doc *scippb.Document) error {
			return visitDocument(ctx, doc, lang, h)
		},
	}
	if err := visitor.ParseStreaming(ctx, f); err != nil {
		return fmt.Errorf("parse scip index: %w", err)
	}
	return nil
}

// visitDocument processes a single SCIP document (source file).
func visitDocument(ctx context.Context, doc *scippb.Document, lang string, h Handler) error {
	fileNode := graph.FileNode(doc.GetRelativePath(), lang, "" /* checksum filled by caller */)

	if err := h.OnFile(ctx, fileNode); err != nil {
		return fmt.Errorf("OnFile %s: %w", doc.GetRelativePath(), err)
	}

	// Build a set of symbols defined in this document for reference filtering.
	definedInFile := make(map[string]bool)
	for _, occ := range doc.GetOccurrences() {
		if isDefinition(occ) {
			definedInFile[occ.GetSymbol()] = true
		}
	}

	// Symbols section provides display metadata (kind, documentation) for
	// definitions in this document.
	symMeta := indexSymbolMeta(doc.GetSymbols())

	for _, occ := range doc.GetOccurrences() {
		sym := occ.GetSymbol()
		if sym == "" {
			continue
		}

		if isDefinition(occ) {
			meta := symMeta[sym]
			symNode := symbolNode(sym, lang, meta)
			if err := h.OnSymbol(ctx, fileNode, symNode); err != nil {
				return fmt.Errorf("OnSymbol %s: %w", sym, err)
			}
		} else {
			// Non-definition occurrence = reference.
			if err := h.OnReference(ctx, fileNode, sym); err != nil {
				return fmt.Errorf("OnReference %s: %w", sym, err)
			}
		}
	}
	return nil
}

// isDefinition reports whether a SCIP occurrence carries the Definition role.
func isDefinition(occ *scippb.Occurrence) bool {
	return occ.GetSymbolRoles()&int32(scippb.SymbolRole_Definition) != 0
}

// symbolMeta holds lightweight metadata extracted from SymbolInformation.
type symbolMeta struct {
	kind scippb.SymbolInformation_Kind
}

// indexSymbolMeta builds a map from symbol string → metadata for fast lookup.
func indexSymbolMeta(syms []*scippb.SymbolInformation) map[string]symbolMeta {
	m := make(map[string]symbolMeta, len(syms))
	for _, si := range syms {
		m[si.GetSymbol()] = symbolMeta{kind: si.GetKind()}
	}
	return m
}

// symbolNode creates a graph.Node for a SCIP symbol occurrence.
// Visibility is inferred from the symbol string: SCIP local symbols (starting
// with "local ") are private; everything else is treated as potentially public.
// Full visibility determination requires language-specific rules and is
// refined by downstream callers (TASK-007 boundary logic).
func symbolNode(scipSymbol, lang string, meta symbolMeta) *graph.Node {
	vis := graph.VisPublic
	if len(scipSymbol) > 6 && scipSymbol[:6] == "local " {
		vis = graph.VisPrivate
	}

	kind := scipKindToGraphKind(meta.kind)
	// Use the last segment of the SCIP symbol as the display name.
	name := scipSymbolName(scipSymbol)

	return graph.SymbolNode(name, lang, kind, vis)
}

// scipKindToGraphKind maps SCIP SymbolInformation_Kind to graph.SymbolKind.
// Unknown/unmapped kinds default to KindFunction as a safe fallback.
func scipKindToGraphKind(k scippb.SymbolInformation_Kind) graph.SymbolKind {
	switch k {
	case scippb.SymbolInformation_Function, scippb.SymbolInformation_Method:
		return graph.KindFunction
	case scippb.SymbolInformation_Class, scippb.SymbolInformation_Struct:
		return graph.KindType
	case scippb.SymbolInformation_Interface:
		return graph.KindInterface
	case scippb.SymbolInformation_Variable:
		return graph.KindVariable
	case scippb.SymbolInformation_Constant, scippb.SymbolInformation_EnumMember:
		return graph.KindConstant
	default:
		return graph.KindFunction
	}
}

// scipSymbolName extracts the human-readable name from a SCIP symbol string.
// SCIP symbols follow the format: "<scheme> <package> <descriptor>".
// We take the last descriptor segment as the name.
func scipSymbolName(sym string) string {
	if sym == "" {
		return sym
	}
	// Find last '.' or '/' segment — that's typically the method/function name.
	for i := len(sym) - 1; i >= 0; i-- {
		switch sym[i] {
		case '.', '/', '#':
			name := sym[i+1:]
			// Strip trailing punctuation used in SCIP descriptors (e.g. trailing '.', '#')
			for len(name) > 0 {
				last := name[len(name)-1]
				if last == '.' || last == '(' || last == ')' || last == '#' {
					name = name[:len(name)-1]
				} else {
					break
				}
			}
			if name != "" {
				return name
			}
		}
	}
	return sym
}
