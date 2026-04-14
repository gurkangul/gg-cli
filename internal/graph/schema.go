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

// ResolutionTier records how a graph node was produced.
//
// Tier contract (TASK-014):
//
//   - Semantic (SCIP tier): authoritative. Boundary nodes, cross-file refs,
//     and transitive --changed closure are only valid for semantic nodes.
//     Produced by scip-go / scip-typescript / scip-python.
//
//   - Syntactic (tree-sitter tier): syntactic-only. File-level invalidation,
//     function/block boundary — but NO cross-file refs. Used as a fallback
//     for languages SCIP doesn't cover.
//
// Contamination rule: BoundarySymbols and cross-file ref queries MUST filter
// WHERE resolution = "semantic". Tree-sitter nodes must never appear in
// authoritative API surface results. The two pipelines write different nodes
// with different resolution values — they must not merge-write the same node.
type ResolutionTier string

const (
	// ResolutionSemantic marks a node produced by a SCIP indexer.
	// These nodes are authoritative: they carry accurate cross-file refs,
	// correct boundary detection, and participate in transitive closure.
	ResolutionSemantic ResolutionTier = "semantic"

	// ResolutionSyntactic marks a node produced by a tree-sitter parser.
	// These nodes are file-scoped and syntactic-only: no cross-file refs,
	// no guarantee of correct boundary detection, and excluded from
	// BoundarySymbols and cross-file queries.
	ResolutionSyntactic ResolutionTier = "syntactic"
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
//
// tier specifies which indexer produced this node. Boundary queries filter
// to ResolutionSemantic only — syntactic nodes are never authoritative.
func SymbolNode(name, lang string, kind SymbolKind, vis Visibility, tier ResolutionTier) *Node {
	return &Node{
		Label: LabelSymbol,
		Properties: map[string]any{
			"name":       name,
			"lang":       lang,
			"kind":       string(kind),
			"visibility": string(vis),
			"boundary":   vis == VisPublic,
			"resolution": string(tier),
		},
	}
}

// FileNode creates a Node representing a source file.
// checksum is a content hash (e.g. SHA-256 hex) used for change detection.
// tier indicates which indexer processed this file (semantic = SCIP, syntactic = tree-sitter).
func FileNode(path, lang, checksum string, tier ResolutionTier) *Node {
	return &Node{
		Label: LabelFile,
		Properties: map[string]any{
			"path":       path,
			"lang":       lang,
			"checksum":   checksum,
			"resolution": string(tier),
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
//   - Symbol(project_id) — isolate queries by project (the primary isolation index)
//   - Symbol(name)       — fast lookup by symbol name within a project
//   - Symbol(boundary)   — enumerate all boundary symbols within a project
//   - File(project_id)   — file lookup within a project
//   - File(path)         — deduplicate / lookup files by path
//   - Package(import_path) — deduplicate packages by canonical import path
func (c *Client) SchemaInit(ctx context.Context) error {
	indexes := []string{
		"CREATE INDEX ON :Symbol(project_id)",
		"CREATE INDEX ON :Symbol(name)",
		"CREATE INDEX ON :Symbol(boundary)",
		"CREATE INDEX ON :Symbol(resolution)", // tier filter for BoundarySymbols + cross-file queries
		"CREATE INDEX ON :File(project_id)",
		"CREATE INDEX ON :File(path)",
		"CREATE INDEX ON :File(resolution)", // tier filter for --changed scoping
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

// BoundarySymbols returns all Symbol nodes with boundary=true scoped to this
// client's project. These represent the exported API surface of the indexed
// codebase for this project only.
func (c *Client) BoundarySymbols(ctx context.Context) ([]*Node, error) {
	sess := c.session(ctx)
	defer sess.Close(ctx)

	// resolution = "semantic" enforces the hybrid tier contract: syntactic
	// (tree-sitter) nodes are never authoritative API surface.
	result, err := sess.Run(ctx,
		"MATCH (n:Symbol {boundary: true, project_id: $pid, resolution: $tier}) "+
			"RETURN toString(id(n)) AS id, properties(n) AS props",
		map[string]any{"pid": c.projectID, "tier": string(ResolutionSemantic)},
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
