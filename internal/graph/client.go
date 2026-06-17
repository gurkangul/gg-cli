package graph

import (
	"context"
	"fmt"
	"os"
)

// Client is the project-scoped facade over the embedded graph store.
//
// The backend is the embedded, CGO-free SQLite GraphStore (graph.db) — gg has no
// graph server anymore. The Client is backend-agnostic in shape: it builds Cypher
// and consumes the backend-neutral GraphResult, with the choke-point in queries.go.
//
// Per-project isolation: every node carries a project_id property injected
// automatically by CreateNode. All queries filter on project_id so multiple
// projects can share one graph.db without seeing each other's data.
type Client struct {
	gs        GraphStore // embedded SQLite graph store
	projectID string     // per-project namespace — set on every created node
}

// New creates a graph client. projectID must be a non-empty string identifying
// this project (e.g. the UUID from config.ProjectID). All nodes written by this
// client carry {project_id: projectID} and all queries filter on it. Call
// Close() when done.
//
// The graph lives in the embedded SQLite store graph.db, opened under dataDir
// (the project .gg directory; falling back to <cwd>/.gg when empty).
func New(dataDir, projectID string) (*Client, error) {
	if projectID == "" {
		return nil, fmt.Errorf("projectID is required — config missing project_id")
	}
	gs, err := newGraphStore(graphDataDir(dataDir))
	if err != nil {
		return nil, err
	}
	return &Client{gs: gs, projectID: projectID}, nil
}

// newGraphStore constructs the embedded SQLite GraphStore at dataDir.
func newGraphStore(dataDir string) (GraphStore, error) {
	ss, err := newSQLiteStore(dataDir)
	if err != nil {
		return nil, fmt.Errorf("sqlite graph store init failed: %w", err)
	}
	return ss, nil
}

// graphDataDir resolves the directory the embedded SQLite graph database lives
// in. It prefers the supplied dataDir; otherwise it uses <cwd>/.gg so the
// embedded graph is co-located with the rest of the project store.
func graphDataDir(dataDir string) string {
	if dataDir != "" {
		return dataDir
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
