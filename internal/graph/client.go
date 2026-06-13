package graph

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/gurkangul/gg-cli/internal/config"
)

// Client is the project-scoped facade over the configured graph backend.
//
// The backend is a GraphStore (TASK-494): either Memgraph reached over Bolt with
// the neo4j driver (the default and parity oracle) or the embedded, CGO-free
// SQLite store. The Client is backend-agnostic — it builds Cypher and consumes
// the backend-neutral GraphResult, with the choke-point in queries.go.
//
// Per-project isolation: every node carries a project_id property injected
// automatically by CreateNode. All queries filter on project_id so multiple
// projects can share the same backend without seeing each other's data —
// mirroring the Qdrant collection-prefix pattern in the store package.
type Client struct {
	gs        GraphStore // backend-agnostic graph store (Memgraph or SQLite)
	projectID string     // per-project namespace — set on every created node
}

// GraphBackendEnv selects the graph-store backend. Unset, "memgraph" or "neo4j"
// keeps the historical Memgraph/Bolt path (default); "sqlite" activates the
// embedded, CGO-free store (TASK-494) that removes the Memgraph Docker
// dependency.
const GraphBackendEnv = "GG_GRAPH_BACKEND"

// New creates a graph client from config. projectID must be a non-empty string
// identifying this project (e.g. the UUID from config.ProjectID). All nodes
// written by this client carry {project_id: projectID} and all queries filter on
// it. Call Close() when done.
//
// The backend is selected by the GG_GRAPH_BACKEND env var (default "memgraph").
// With "sqlite", cfg.URI is ignored and an embedded graph.db is opened under the
// project .gg directory derived from cfg.
func New(cfg *config.MemgraphConfig, projectID string) (*Client, error) {
	if projectID == "" {
		return nil, fmt.Errorf("projectID is required — config missing project_id")
	}
	gs, err := newGraphStore(cfg, projectID)
	if err != nil {
		return nil, err
	}
	return &Client{gs: gs, projectID: projectID}, nil
}

// newGraphStore constructs the configured GraphStore. The SQLite backend stores
// its database under cfg.DataDir (the project .gg directory); when that is empty
// it falls back to the current working directory's .gg so existing call-sites
// that only populate URI keep working in tests and live use.
func newGraphStore(cfg *config.MemgraphConfig, projectID string) (GraphStore, error) {
	backend := strings.ToLower(strings.TrimSpace(os.Getenv(GraphBackendEnv)))
	if backend == "sqlite" {
		ss, err := newSQLiteStore(graphDataDir(cfg))
		if err != nil {
			return nil, fmt.Errorf("sqlite graph store init failed: %w", err)
		}
		return ss, nil
	}
	ns, err := newNeo4jStore(cfg)
	if err != nil {
		return nil, err
	}
	return ns, nil
}

// graphDataDir resolves the directory the embedded SQLite graph database lives
// in. It prefers cfg.DataDir; otherwise it uses <cwd>/.gg so the embedded graph
// is co-located with the rest of the project store.
func graphDataDir(cfg *config.MemgraphConfig) string {
	if cfg != nil && cfg.DataDir != "" {
		return cfg.DataDir
	}
	if wd, err := os.Getwd(); err == nil {
		return wd + string(os.PathSeparator) + ".gg"
	}
	return ".gg"
}

// Close releases backend resources. Safe to call multiple times.
func (c *Client) Close(ctx context.Context) error {
	return c.gs.Close(ctx)
}

// HealthCheck verifies the backend is reachable.
func (c *Client) HealthCheck(ctx context.Context) error {
	return c.gs.HealthCheck(ctx)
}
