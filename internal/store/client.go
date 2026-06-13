package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/trace"
	"github.com/qdrant/go-client/qdrant"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ErrQdrantDown is returned by read operations (Query, Scroll) when Qdrant
// is unreachable. Write operations (Upsert) return this error too, but callers
// should treat it as fatal for mutations — writes that silently succeed
// nowhere are worse than an explicit failure.
var ErrQdrantDown = errors.New("qdrant unreachable")

const (
	collSuffixDecisions   = "decisions"
	collSuffixTasks       = "tasks"
	collSuffixMessages    = "messages"
	collSuffixRejections  = "rejections"
	collSuffixDiscussions = "discussions"
	collSuffixNotes       = "notes"
	collSuffixBugs        = "bugs"

	VectorSize = 768 // nomic-embed-text dimension
)

// scrollerIface is the minimal Qdrant scroll interface used by ExportBrainCollection
// and embedMissingInCollection. It exists so unit tests can inject a fake.
type scrollerIface interface {
	ScrollAndOffset(ctx context.Context, req *qdrant.ScrollPoints) ([]*qdrant.RetrievedPoint, *qdrant.PointId, error)
}

type Client struct {
	vs        VectorStore   // backend-agnostic vector index (Qdrant or SQLite)
	scroller  scrollerIface // defaults to vs; overridable in tests
	dataDir   string        // absolute path to project-local .gg/ — used for file-locked counters
	projectID string        // per-project namespace prefix for collections
}

// VectorBackendEnv selects the vector-store backend, overriding the config field.
// "sqlite" (the built-in default as of TASK-496) activates the embedded,
// CGO-free brute-force store; "qdrant" keeps the historical Qdrant server path.
// Mirrors config.VectorBackendEnv.
const VectorBackendEnv = config.VectorBackendEnv

// New creates a store client bound to a specific project. Collection names are
// prefixed with projectID so multiple projects can share a single backend
// without seeing each other's data.
//
// The backend is resolved with precedence GG_VECTOR_BACKEND env > qdrant.backend
// config field > sqlite (the embedded default — no Docker). With the embedded
// backend the cfg host/port are ignored and vectorstore.db is opened under
// dataDir; only an explicit "qdrant" selection dials the server.
func New(cfg *config.QdrantConfig, dataDir, projectID string) (*Client, error) {
	if projectID == "" {
		return nil, fmt.Errorf("project ID is required — config missing project_id")
	}
	if cfg.UsesEmbedded() {
		ss, err := newSQLiteStore(dataDir)
		if err != nil {
			return nil, fmt.Errorf("sqlite vector store init failed: %w", err)
		}
		return &Client{vs: ss, scroller: ss, dataDir: dataDir, projectID: projectID}, nil
	}
	qs, err := newQdrantStore(cfg)
	if err != nil {
		return nil, fmt.Errorf("qdrant connect failed: %w", err)
	}
	return &Client{vs: qs, scroller: qs, dataDir: dataDir, projectID: projectID}, nil
}

func (c *Client) Close() error {
	return c.vs.Close()
}

func (c *Client) HealthCheck(ctx context.Context) error {
	return c.vs.HealthCheck(ctx)
}

// Collection names are composed of <project_id>-<type>. The project_id is a
// UUID, safe for collection names.
func (c *Client) collDecisions() string   { return c.projectID + "-" + collSuffixDecisions }
func (c *Client) collTasks() string       { return c.projectID + "-" + collSuffixTasks }
func (c *Client) collMessages() string    { return c.projectID + "-" + collSuffixMessages }
func (c *Client) collRejections() string  { return c.projectID + "-" + collSuffixRejections }
func (c *Client) collDiscussions() string { return c.projectID + "-" + collSuffixDiscussions }
func (c *Client) collNotes() string       { return c.projectID + "-" + collSuffixNotes }
func (c *Client) collBugs() string        { return c.projectID + "-" + collSuffixBugs }

// CollectionStatus reports which of the expected Qdrant collections are
// present vs. missing for this project. It returns (present, missing, error).
func (c *Client) CollectionStatus(ctx context.Context) (present, missing []string, err error) {
	expected := []string{
		c.collDecisions(), c.collTasks(), c.collMessages(),
		c.collRejections(), c.collDiscussions(), c.collNotes(), c.collBugs(),
	}
	all, err := c.vs.ListCollections(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("list collections: %w", err)
	}
	existMap := make(map[string]bool, len(all))
	for _, name := range all {
		existMap[name] = true
	}
	for _, name := range expected {
		if existMap[name] {
			present = append(present, name)
		} else {
			missing = append(missing, name)
		}
	}
	return present, missing, nil
}

// EnsureCollections creates this project's Qdrant collections if they don't exist.
// vectorSize is the embedding dimension for the project — pass the value from the
// embedding meta file (or store.VectorSize as a default for first-time setups).
func (c *Client) EnsureCollections(ctx context.Context, vectorSize uint64) error {
	collections := []string{c.collDecisions(), c.collTasks(), c.collMessages(), c.collRejections(), c.collDiscussions(), c.collNotes(), c.collBugs()}
	existing, err := c.vs.ListCollections(ctx)
	if err != nil {
		return fmt.Errorf("list collections: %w", err)
	}
	existMap := make(map[string]bool)
	for _, name := range existing {
		existMap[name] = true
	}

	vectorCfg := qdrant.NewVectorsConfig(&qdrant.VectorParams{
		Size:     vectorSize,
		Distance: qdrant.Distance_Cosine,
	})
	for _, name := range collections {
		if existMap[name] {
			continue
		}
		err := c.vs.CreateCollection(ctx, &qdrant.CreateCollection{
			CollectionName: name,
			VectorsConfig:  vectorCfg,
		})
		if err != nil {
			return fmt.Errorf("create collection %s: %w", name, err)
		}
	}
	return nil
}

// qdrantUpsert is the single choke-point for all Qdrant upsert calls in this
// package. It wraps qc.Upsert with a trace span so GG_TRACE=1 captures latency.
// Returns ErrQdrantDown when the error is a network connectivity failure so
// callers can distinguish "Qdrant is down" from "bad request".
func (c *Client) qdrantUpsert(ctx context.Context, req *qdrant.UpsertPoints) error {
	start := time.Now()
	_, err := c.vs.Upsert(ctx, req)
	trace.Record("store.upsert", start, err)
	if err != nil && isConnectivityError(err) {
		return fmt.Errorf("%w: %v", ErrQdrantDown, err)
	}
	return err
}

// qdrantQuery is the single choke-point for all Qdrant vector query calls in
// this package. It wraps qc.Query with a trace span.
// Returns ErrQdrantDown when the error is a network connectivity failure.
func (c *Client) qdrantQuery(ctx context.Context, req *qdrant.QueryPoints) ([]*qdrant.ScoredPoint, error) {
	start := time.Now()
	results, err := c.vs.Query(ctx, req)
	trace.Record("store.query", start, err)
	if err != nil && isConnectivityError(err) {
		return nil, fmt.Errorf("%w: %v", ErrQdrantDown, err)
	}
	return results, err
}

// isConnectivityError reports whether err looks like a true network-level
// failure (connection refused, host unreachable, DNS failure, gRPC Unavailable).
// These are distinct from:
//   - Qdrant-level errors (wrong collection, bad vector dimension) — real errors
//   - Context timeouts ("context deadline exceeded") — Qdrant is slow, not down;
//     those should be surfaced separately so callers can ask the user to retry
//     rather than silently serving stale cache data.
func isConnectivityError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, pattern := range []string{
		"connection refused",
		"no such host",
		"network is unreachable",
		"dial tcp",
		"transport is closing",
		"code = unavailable",
		"failed to connect",
	} {
		if strings.Contains(msg, pattern) {
			return true
		}
	}
	return false
}

// isTimeoutError reports whether err is a context deadline or timeout — Qdrant
// is reachable but responding slowly. Callers should surface this distinctly
// from a full connectivity failure rather than silently falling back to cache.
func isTimeoutError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "context deadline exceeded") ||
		strings.Contains(msg, "deadline exceeded")
}

// isCollectionNotFoundError reports whether err (or any cause in its chain)
// carries gRPC status code NotFound. Qdrant returns NotFound when the collection
// does not exist (expected on a fresh install). Every other error code is
// either transient (Unavailable, DeadlineExceeded) or fatal (InvalidArgument,
// PermissionDenied) and must not be silently swallowed.
func isCollectionNotFoundError(err error) bool {
	for err != nil {
		if st, ok := status.FromError(err); ok && st.Code() == codes.NotFound {
			return true
		}
		err = errors.Unwrap(err)
	}
	return false
}

// IsCollectionNotFoundError is the exported form of isCollectionNotFoundError so
// command-layer read paths can treat a missing collection (fresh install that
// hasn't run `gg reembed`) the same as a store-down condition and fall back to
// the offline JSONL scan instead of surfacing a raw NotFound. With the embedded
// SQLite backend the store is always "up", so this is the only signal that the
// collection has not been materialized yet.
func IsCollectionNotFoundError(err error) bool {
	return isCollectionNotFoundError(err)
}

// DropAllCollections deletes all project collections from Qdrant.
// Used by `gg reembed` before recreating collections with a new vector size.
func (c *Client) DropAllCollections(ctx context.Context) error {
	collections := []string{c.collDecisions(), c.collTasks(), c.collMessages(), c.collRejections(), c.collDiscussions(), c.collNotes(), c.collBugs()}
	for _, name := range collections {
		if err := c.vs.DeleteCollection(ctx, name); err != nil {
			return fmt.Errorf("drop collection %s: %w", name, err)
		}
	}
	return nil
}
