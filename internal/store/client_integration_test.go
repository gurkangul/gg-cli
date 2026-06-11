// Package store — integration tests for the Client connection/health surface in
// client.go. These exercise the real Qdrant gRPC API and are gated on
// GG_INTEGRATION_TEST=1 so the normal `go test ./...` gate stays green without a
// live Qdrant. The connectivity error-path test does NOT require a live Qdrant;
// it dials a closed port and runs unconditionally.
//
//	GG_INTEGRATION_TEST=1 go test ./internal/store/ -run 'Client.*Integration' -v -count=1
package store

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/gurkangul/gg-cli/internal/config"
)

// TestClientHealthCheck_Integration verifies a live Qdrant answers HealthCheck.
func TestClientHealthCheck_Integration(t *testing.T) {
	c, ctx := newIntegrationClient(t, "client-health")
	if err := c.HealthCheck(ctx); err != nil {
		t.Fatalf("HealthCheck against live Qdrant: %v", err)
	}
}

// TestClientCollectionStatus_Integration verifies that after EnsureCollections
// (run by the helper) all 7 project collections are reported present and none
// missing — i.e. ListCollections under the hood sees them.
func TestClientCollectionStatus_Integration(t *testing.T) {
	c, ctx := newIntegrationClient(t, "client-collstatus")

	present, missing, err := c.CollectionStatus(ctx)
	if err != nil {
		t.Fatalf("CollectionStatus: %v", err)
	}
	if len(present) != 7 {
		t.Fatalf("present collections = %d, want 7: %v", len(present), present)
	}
	if len(missing) != 0 {
		t.Fatalf("missing collections = %v, want none (helper ran EnsureCollections)", missing)
	}
	// Each present name must carry this project's prefix — proves isolation.
	for _, name := range present {
		if !strings.HasPrefix(name, c.projectID+"-") {
			t.Fatalf("collection %q is not prefixed with project ID %q", name, c.projectID)
		}
	}
}

// TestClientCollectionStatus_DropMakesMissing_Integration verifies the missing
// branch: after dropping all collections, CollectionStatus reports all 7 missing.
func TestClientCollectionStatus_DropMakesMissing_Integration(t *testing.T) {
	c, ctx := newIntegrationClient(t, "client-drop")

	if err := c.DropAllCollections(ctx); err != nil {
		t.Fatalf("DropAllCollections: %v", err)
	}
	present, missing, err := c.CollectionStatus(ctx)
	if err != nil {
		t.Fatalf("CollectionStatus after drop: %v", err)
	}
	if len(present) != 0 {
		t.Fatalf("present after drop = %v, want none", present)
	}
	if len(missing) != 7 {
		t.Fatalf("missing after drop = %d, want 7: %v", len(missing), missing)
	}
}

// TestClientHealthCheck_ContextCanceled_Integration verifies that a canceled
// context is honored: HealthCheck must return an error rather than blocking or
// silently succeeding. Requires live Qdrant so the only failure source is the
// canceled context.
func TestClientHealthCheck_ContextCanceled_Integration(t *testing.T) {
	c, _ := newIntegrationClient(t, "client-ctxcancel")

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before the call

	err := c.HealthCheck(ctx)
	if err == nil {
		t.Fatal("HealthCheck with canceled context returned nil, want context error")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "context canceled") {
		t.Fatalf("HealthCheck error = %v, want context canceled", err)
	}
}

// TestClientHealthCheck_QdrantDown verifies the connectivity error path WITHOUT a
// live Qdrant: dialing a port with nothing listening must fail (not hang past the
// short deadline, not silently succeed). This guards the isConnectivityError seam
// used by qdrantUpsert/qdrantQuery. Runs unconditionally — no GG_INTEGRATION_TEST.
func TestClientHealthCheck_QdrantDown(t *testing.T) {
	c, err := New(&config.QdrantConfig{Host: "127.0.0.1", Port: 19998}, t.TempDir(), "test-client-down")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := c.HealthCheck(ctx); err == nil {
		t.Fatal("HealthCheck against dead port returned nil, want connectivity error")
	}
}

// TestClientCollectionStatus_QdrantDown verifies CollectionStatus surfaces a
// ListCollections failure (does not pretend everything is missing/empty) when
// Qdrant is unreachable. Runs unconditionally — no live Qdrant required.
func TestClientCollectionStatus_QdrantDown(t *testing.T) {
	c, err := New(&config.QdrantConfig{Host: "127.0.0.1", Port: 19998}, t.TempDir(), "test-client-down")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	present, missing, err := c.CollectionStatus(ctx)
	if err == nil {
		t.Fatal("CollectionStatus against dead port returned nil error, want list failure")
	}
	if present != nil || missing != nil {
		t.Fatalf("CollectionStatus on failure returned present=%v missing=%v, want nil/nil", present, missing)
	}
}
