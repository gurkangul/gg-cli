package config

import (
	"os"
	"strings"
)

// Storage-backend identifiers. The embedded SQLite backends are the default as
// of TASK-496 (no Docker required); the server-backed Qdrant/Memgraph backends
// stay fully selectable for users who prefer running the servers.
const (
	// BackendSQLite is the embedded, CGO-free default for both the vector store
	// (vectorstore.db) and the graph store (graph.db). No Docker container is
	// needed.
	BackendSQLite = "sqlite"
	// BackendQdrant selects the historical Qdrant server vector backend.
	BackendQdrant = "qdrant"
	// BackendMemgraph selects the historical Memgraph server graph backend.
	// "neo4j" is accepted as an alias by the graph client.
	BackendMemgraph = "memgraph"

	// VectorBackendEnv overrides the resolved vector backend at runtime. It wins
	// over the config field. Mirrors store.VectorBackendEnv (kept in sync).
	VectorBackendEnv = "GG_VECTOR_BACKEND"
	// GraphBackendEnv overrides the resolved graph backend at runtime. It wins
	// over the config field. Mirrors graph.GraphBackendEnv (kept in sync).
	GraphBackendEnv = "GG_GRAPH_BACKEND"
)

// resolveBackend implements the shared precedence used by both stores:
//
//	explicit env var > config field > built-in default (sqlite)
//
// Empty/whitespace at any layer falls through to the next. The result is always
// lower-cased and trimmed so downstream comparisons stay simple.
func resolveBackend(envVar, configField, builtinDefault string) string {
	if v := strings.ToLower(strings.TrimSpace(os.Getenv(envVar))); v != "" {
		return v
	}
	if v := strings.ToLower(strings.TrimSpace(configField)); v != "" {
		return v
	}
	return builtinDefault
}

// ResolveBackend returns the effective vector backend for this QdrantConfig with
// precedence GG_VECTOR_BACKEND env > qdrant.backend config field > sqlite. As of
// TASK-496 the built-in default is the embedded SQLite store, so a project that
// sets nothing runs with no Qdrant container.
func (q *QdrantConfig) ResolveBackend() string {
	field := ""
	if q != nil {
		field = q.Backend
	}
	return resolveBackend(VectorBackendEnv, field, BackendSQLite)
}

// UsesEmbedded reports whether the resolved vector backend is the embedded
// SQLite store (the default). Doctor/init use this to pick embedded-health vs.
// server-connectivity checks.
func (q *QdrantConfig) UsesEmbedded() bool {
	return q.ResolveBackend() == BackendSQLite
}

// ResolveBackend returns the effective graph backend for this MemgraphConfig
// with precedence GG_GRAPH_BACKEND env > memgraph.backend config field > sqlite.
// "neo4j" and "memgraph" both select the server backend.
func (m *MemgraphConfig) ResolveBackend() string {
	field := ""
	if m != nil {
		field = m.Backend
	}
	return resolveBackend(GraphBackendEnv, field, BackendSQLite)
}

// UsesEmbedded reports whether the resolved graph backend is the embedded SQLite
// store (the default).
func (m *MemgraphConfig) UsesEmbedded() bool {
	return m.ResolveBackend() == BackendSQLite
}
