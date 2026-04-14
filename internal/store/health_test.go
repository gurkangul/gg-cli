package store

import (
	"context"
	"testing"
	"time"

	"github.com/gurkangul/gg-cli/internal/config"
)

// TestHealthCheck_Unreachable verifies that HealthCheck returns a non-nil
// error when Qdrant is not listening. This exercises the code path that
// loadDeps wraps with a user-friendly "is Qdrant running?" message.
//
// Port 19999 is used as a "nothing there" port — not a well-known service.
// The timeout is kept short so the test doesn't slow down CI.
func TestHealthCheck_Unreachable(t *testing.T) {
	cfg := &config.QdrantConfig{
		Host: "127.0.0.1",
		Port: 19999, // no service listening here
	}
	c, err := New(cfg, t.TempDir(), "test-project-healthcheck")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = c.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := c.HealthCheck(ctx); err == nil {
		t.Fatal("expected error from HealthCheck when Qdrant is unreachable, got nil")
	}
}
