package graph

import (
	"context"
	"fmt"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// Node labels used in the Memgraph graph schema.
// Keep these in sync with the Cypher indexes below.
const (
	LabelSymbol  = "Symbol"
	LabelFile    = "File"
	LabelPackage = "Package"
)

// Relationship types between nodes.
const (
	RelDefines  = "DEFINES"  // (File)-[:DEFINES]->(Symbol)
	RelContains = "CONTAINS" // (Package)-[:CONTAINS]->(File)
	RelCalls    = "CALLS"    // (Symbol)-[:CALLS]->(Symbol)
	RelImports  = "IMPORTS"  // (Symbol|File)-[:IMPORTS]->(Package)
)

// SymbolKind classifies the kind of code symbol a Symbol node represents.
type SymbolKind string

const (
	KindFunction  SymbolKind = "function"
	KindMethod    SymbolKind = "method"
	KindType      SymbolKind = "type"
	KindVariable  SymbolKind = "var"
	KindConstant  SymbolKind = "const"
	KindInterface SymbolKind = "interface"
)

// Visibility encodes whether a symbol is accessible outside its package.
type Visibility string

const (
	VisPublic  Visibility = "public"  // exported — forms the package boundary
	VisPrivate Visibility = "private" // unexported
)

// SymbolNode creates a Node for a code symbol.
//
// boundary indicates whether this symbol is part of the package's external
// interface (i.e. exported/public). Boundary symbols are Day-1 scope for the
// graph knowledge base: they're the contracts other packages depend on.
func SymbolNode(name, lang string, kind SymbolKind, vis Visibility) *Node {
	return &Node{
		Label: LabelSymbol,
		Properties: map[string]any{
			"name":       name,
			"lang":       lang,
			"kind":       string(kind),
			"visibility": string(vis),
			"boundary":   vis == VisPublic,
		},
	}
}

// FileNode creates a Node representing a source file.
// checksum is a content hash (e.g. SHA-256 hex) used for change detection.
func FileNode(path, lang, checksum string) *Node {
	return &Node{
		Label: LabelFile,
		Properties: map[string]any{
			"path":     path,
			"lang":     lang,
			"checksum": checksum,
		},
	}
}

// PackageNode creates a Node representing a package or module.
// importPath is the canonical import identifier (e.g. "github.com/gurkangul/gg/internal/graph").
func PackageNode(name, lang, importPath string) *Node {
	return &Node{
		Label: LabelPackage,
		Properties: map[string]any{
			"name":        name,
			"lang":        lang,
			"import_path": importPath,
		},
	}
}

// SchemaInit creates Memgraph indexes required for efficient graph queries.
// Safe to call on an already-initialised schema — Memgraph CREATE INDEX IF NOT EXISTS.
//
// Indexes created:
//   - Symbol(name)       — fast lookup by symbol name
//   - Symbol(boundary)   — enumerate all boundary symbols across the codebase
//   - File(path)         — deduplicate / lookup files by path
//   - Package(import_path) — deduplicate packages by canonical import path
func (c *Client) SchemaInit(ctx context.Context) error {
	indexes := []string{
		"CREATE INDEX ON :Symbol(name)",
		"CREATE INDEX ON :Symbol(boundary)",
		"CREATE INDEX ON :File(path)",
		"CREATE INDEX ON :Package(import_path)",
	}

	sess := c.session(ctx)
	defer sess.Close(ctx)

	for _, cypher := range indexes {
		_, err := sess.Run(ctx, cypher, nil)
		if err != nil {
			return fmt.Errorf("schema init %q: %w", cypher, err)
		}
	}
	return nil
}

// BoundarySymbols returns all Symbol nodes with boundary=true.
// These represent the exported API surface of the indexed codebase.
func (c *Client) BoundarySymbols(ctx context.Context) ([]*Node, error) {
	sess := c.session(ctx)
	defer sess.Close(ctx)

	result, err := sess.Run(ctx,
		"MATCH (n:Symbol {boundary: true}) RETURN toString(id(n)) AS id, properties(n) AS props",
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("boundary symbols query: %w", err)
	}

	var nodes []*Node
	for result.Next(ctx) {
		record := result.Record()
		id, _, _ := neo4j.GetRecordValue[string](record, "id")
		props, _, _ := neo4j.GetRecordValue[map[string]any](record, "props")
		nodes = append(nodes, &Node{ID: id, Label: LabelSymbol, Properties: props})
	}
	if err := result.Err(); err != nil {
		return nil, fmt.Errorf("boundary symbols iterate: %w", err)
	}
	return nodes, nil
}
