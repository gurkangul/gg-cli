package store

// Tests for EnsureCollections idempotency and pre-existing-config scenario.
//
// The _QdrantDown variant runs without any external service. The integration
// variants require a live Qdrant and are gated on GG_INTEGRATION_TEST=1:
//
//	GG_INTEGRATION_TEST=1 go test ./internal/store/ -run TestEnsureCollections -v -count=1

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gurkangul/gg-cli/internal/config"
)

// TestEnsureCollections_Idempotent verifies that calling EnsureCollections
// twice on the same project does not fail when the collections already exist.
// Requires live Qdrant; skipped unless GG_INTEGRATION_TEST=1.
func TestEnsureCollections_Idempotent(t *testing.T) {
	if os.Getenv("GG_INTEGRATION_TEST") != "1" {
		t.Skip("set GG_INTEGRATION_TEST=1 to run Qdrant integration tests")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	projectID := "test-ensure-idem-" + uuid.New().String()[:8]
	cfg := &config.QdrantConfig{Backend: config.BackendQdrant, Host: "127.0.0.1", Port: 6334}
	c, err := New(cfg, t.TempDir(), projectID)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	if err := c.HealthCheck(ctx); err != nil {
		t.Skipf("Qdrant not reachable: %v", err)
	}
	t.Cleanup(func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanCancel()
		_ = c.DropAllCollections(cleanCtx)
	})

	// First call — creates all 7 collections.
	if err := c.EnsureCollections(ctx, VectorSize); err != nil {
		t.Fatalf("EnsureCollections (first): %v", err)
	}

	// Second call — collections already exist; must not error.
	if err := c.EnsureCollections(ctx, VectorSize); err != nil {
		t.Fatalf("EnsureCollections (second, idempotent): %v", err)
	}

	present, missing, err := c.CollectionStatus(ctx)
	if err != nil {
		t.Fatalf("CollectionStatus: %v", err)
	}
	if len(missing) != 0 {
		t.Errorf("expected all collections present after two EnsureCollections calls; missing: %v", missing)
	}
	if len(present) != 7 {
		t.Errorf("expected 7 collections present, got %d: %v", len(present), present)
	}
}

// TestEnsureCollections_PreExistingConfig verifies the scenario where a project
// config.yaml with a custom project_id was written before EnsureCollections is
// called (the brownfield / smoke-script foot-gun that TASK-349 fixes).
//
// The test simulates: pre-write config → create client → call EnsureCollections
// → verify all expected collections are created under the custom project_id.
// Requires live Qdrant; skipped unless GG_INTEGRATION_TEST=1.
func TestEnsureCollections_PreExistingConfig(t *testing.T) {
	if os.Getenv("GG_INTEGRATION_TEST") != "1" {
		t.Skip("set GG_INTEGRATION_TEST=1 to run Qdrant integration tests")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Simulate a pre-written config with a custom project_id (e.g. "smoke-<uuid>").
	customProjectID := "smoke-" + uuid.New().String()[:8]
	cfg := &config.QdrantConfig{Backend: config.BackendQdrant, Host: "127.0.0.1", Port: 6334}

	c, err := New(cfg, t.TempDir(), customProjectID)
	if err != nil {
		t.Fatalf("New with pre-existing project_id: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	if err := c.HealthCheck(ctx); err != nil {
		t.Skipf("Qdrant not reachable: %v", err)
	}
	t.Cleanup(func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanCancel()
		_ = c.DropAllCollections(cleanCtx)
	})

	// At this point no collections exist for customProjectID — simulates the
	// state after pre-writing config.yaml but before the fixed `gg init` runs.
	_, missing, err := c.CollectionStatus(ctx)
	if err != nil {
		t.Fatalf("CollectionStatus (pre-ensure): %v", err)
	}
	if len(missing) != 7 {
		t.Errorf("expected all 7 collections missing before EnsureCollections, got missing=%v", missing)
	}

	// EnsureCollections must create all of them.
	if err := c.EnsureCollections(ctx, VectorSize); err != nil {
		t.Fatalf("EnsureCollections for pre-existing config project_id: %v", err)
	}

	present, missing2, err := c.CollectionStatus(ctx)
	if err != nil {
		t.Fatalf("CollectionStatus (post-ensure): %v", err)
	}
	if len(missing2) != 0 {
		t.Errorf("expected all collections created; still missing: %v", missing2)
	}
	if len(present) != 7 {
		t.Errorf("expected 7 collections present, got %d: %v", len(present), present)
	}
}
