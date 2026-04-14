package graph

import (
	"context"
	"fmt"

	"github.com/gurkangul/gg/internal/config"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// Client wraps the neo4j driver configured to talk to Memgraph via Bolt.
// Memgraph is Bolt-compatible; we use neo4j-go-driver/v5 as the transport.
//
// Per-project isolation: every node carries a project_id property injected
// automatically by CreateNode. All queries filter on project_id so multiple
// projects can share the same Memgraph instance without seeing each other's
// data — mirroring the Qdrant collection-prefix pattern in the store package.
//
// Thread safety: Driver is safe for concurrent use. Sessions must not be
// shared across goroutines — callers should obtain a new session per operation
// (done automatically by the helper methods below).
type Client struct {
	driver    neo4j.DriverWithContext
	projectID string // per-project namespace — set on every created node
}

// New creates a Memgraph client from config. projectID must be a non-empty
// string identifying this project (e.g. the UUID from config.ProjectID).
// All nodes written by this client carry {project_id: projectID} and all
// queries filter on it. Call Close() when done.
func New(cfg *config.MemgraphConfig, projectID string) (*Client, error) {
	if cfg.URI == "" {
		return nil, fmt.Errorf("memgraph.uri is required")
	}
	if projectID == "" {
		return nil, fmt.Errorf("projectID is required — config missing project_id")
	}

	var auth neo4j.AuthToken
	if cfg.Username == "" && cfg.Password == "" {
		auth = neo4j.NoAuth()
	} else {
		auth = neo4j.BasicAuth(cfg.Username, cfg.Password, "")
	}

	driver, err := neo4j.NewDriverWithContext(cfg.URI, auth)
	if err != nil {
		return nil, fmt.Errorf("memgraph driver init: %w", err)
	}
	return &Client{driver: driver, projectID: projectID}, nil
}

// Close releases driver resources. Safe to call multiple times.
func (c *Client) Close(ctx context.Context) error {
	return c.driver.Close(ctx)
}

// HealthCheck verifies the driver can reach Memgraph.
func (c *Client) HealthCheck(ctx context.Context) error {
	return c.driver.VerifyConnectivity(ctx)
}

// session returns a new auto-commit session. The caller must close it.
// We use AccessModeWrite by default; read-only queries run fine on a write session.
func (c *Client) session(ctx context.Context) neo4j.SessionWithContext {
	return c.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode: neo4j.AccessModeWrite,
	})
}
