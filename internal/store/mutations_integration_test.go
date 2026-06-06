package store

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gurkangul/gg-cli/internal/brain"
	"github.com/gurkangul/gg-cli/internal/config"
)

// fakeEmbedder returns a deterministic non-zero vector so reembed can run
// without Ollama. Any record with text gets a stable vector.
type fakeEmbedder struct{}

func (fakeEmbedder) Generate(_ context.Context, text string) ([]float32, error) {
	v := make([]float32, VectorSize)
	if text != "" {
		v[0] = 1
	}
	return v, nil
}

func newIntegrationClient(t *testing.T, name string) (*Client, context.Context) {
	t.Helper()
	if os.Getenv("GG_INTEGRATION_TEST") != "1" {
		t.Skip("set GG_INTEGRATION_TEST=1 to run Qdrant integration tests")
	}
	projectID := fmt.Sprintf("test-%s-%d", name, time.Now().UTC().UnixNano())
	c, err := New(&config.QdrantConfig{Host: "127.0.0.1", Port: 6334}, t.TempDir(), projectID)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	if err := c.HealthCheck(ctx); err != nil {
		t.Fatalf("Qdrant not reachable: %v", err)
	}
	if err := c.EnsureCollections(ctx, VectorSize); err != nil {
		t.Fatalf("EnsureCollections: %v", err)
	}
	t.Cleanup(func() {
		cctx, ccancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer ccancel()
		_ = c.DropAllCollections(cctx)
	})
	return c, ctx
}

// BUG-062 + BUG-063: a decision status mutation must be appended to JSONL (the
// source of truth) with an incremented version, not Qdrant-only.
func TestDecisionMutationIsJSONLFirst_Integration(t *testing.T) {
	c, ctx := newIntegrationClient(t, "dec-mut")
	id := uuid.New().String()
	vec := make([]float32, VectorSize)
	vec[0] = 1
	if err := c.AddDecision(ctx, Decision{ID: id, Text: "use jwt", Reason: "stateless", Status: "active", Author: "tester"}, vec); err != nil {
		t.Fatalf("AddDecision: %v", err)
	}

	if err := c.UpdateDecisionStatus(ctx, id, "superseded"); err != nil {
		t.Fatalf("UpdateDecisionStatus: %v", err)
	}

	// JSONL source of truth reflects the mutation (BUG-062).
	latest, err := brain.ReadLatest(c.dataDir, "decisions")
	if err != nil {
		t.Fatalf("ReadLatest: %v", err)
	}
	var found *brain.Entry
	for i := range latest {
		if latest[i].UUID == id {
			found = &latest[i]
		}
	}
	if found == nil {
		t.Fatal("decision missing from JSONL after mutation")
	}
	if got := found.Payload["status"]; got != "superseded" {
		t.Fatalf("JSONL status = %v, want superseded (mutation never reached JSONL = BUG-062)", got)
	}
	if v := brain.PayloadVersion(found.Payload); v != 2 {
		t.Fatalf("JSONL version = %d, want 2 (create=1, mutation=2)", v)
	}

	// Qdrant mirror also reflects it.
	decs, err := c.ListDecisions(ctx, 0)
	if err != nil {
		t.Fatalf("ListDecisions: %v", err)
	}
	for _, d := range decs {
		if d.ID == id && d.Status != "superseded" {
			t.Fatalf("Qdrant status = %q, want superseded", d.Status)
		}
	}
}

// BUG-062: a bug fix must persist root cause + status to JSONL.
func TestBugFixIsJSONLFirst_Integration(t *testing.T) {
	c, ctx := newIntegrationClient(t, "bug-fix")
	vec := make([]float32, VectorSize)
	vec[0] = 1
	bugID, err := c.ReportBug(ctx, Bug{Title: "leak", Detail: "fd leak", Severity: "high", By: "tester"}, vec)
	if err != nil {
		t.Fatalf("ReportBug: %v", err)
	}
	if err := c.FixBug(ctx, bugID, "missing Close", "added defer Close", "x_test.go"); err != nil {
		t.Fatalf("FixBug: %v", err)
	}
	latest, err := brain.ReadLatest(c.dataDir, "bugs")
	if err != nil {
		t.Fatalf("ReadLatest: %v", err)
	}
	if len(latest) != 1 {
		t.Fatalf("bugs JSONL folded len = %d, want 1", len(latest))
	}
	p := latest[0].Payload
	if p["status"] != "fixed" {
		t.Fatalf("JSONL bug status = %v, want fixed (BUG-062)", p["status"])
	}
	if p["root_cause"] != "missing Close" {
		t.Fatalf("JSONL root_cause = %v, want 'missing Close'", p["root_cause"])
	}
	if brain.PayloadVersion(p) != 2 {
		t.Fatalf("JSONL bug version = %d, want 2", brain.PayloadVersion(p))
	}
}

// BUG-069: reembed must source from JSONL — it must keep a JSONL-only record and
// prefer the JSONL-mutated state over a stale Qdrant payload.
func TestReembedPrefersJSONLSourceOfTruth_Integration(t *testing.T) {
	c, ctx := newIntegrationClient(t, "reembed-src")
	vec := make([]float32, VectorSize)
	vec[0] = 1

	// d1: created in both stores, then mutated (JSONL-first) to superseded.
	d1 := uuid.New().String()
	if err := c.AddDecision(ctx, Decision{ID: d1, Text: "alpha decision", Reason: "r", Status: "active", Author: "tester"}, vec); err != nil {
		t.Fatalf("AddDecision d1: %v", err)
	}
	if err := c.UpdateDecisionStatus(ctx, d1, "superseded"); err != nil {
		t.Fatalf("UpdateDecisionStatus d1: %v", err)
	}

	// d2: JSONL-only record that never reached Qdrant (the BUG-069 drop case).
	d2 := uuid.New().String()
	if err := brain.Append(c.dataDir, "decisions", d2, "tester", map[string]any{
		"text": "beta decision", "reason": "r2", "status": "active",
		"created_at": time.Now().UTC().Format(time.RFC3339), "version": int64(1),
	}); err != nil {
		t.Fatalf("Append d2: %v", err)
	}

	if _, err := c.ReembedAll(ctx, fakeEmbedder{}, VectorSize); err != nil {
		t.Fatalf("ReembedAll: %v", err)
	}

	decs, err := c.ListDecisions(ctx, 0)
	if err != nil {
		t.Fatalf("ListDecisions: %v", err)
	}
	byID := map[string]Decision{}
	for _, d := range decs {
		byID[d.ID] = d
	}
	if d, ok := byID[d1]; !ok {
		t.Fatal("d1 missing after reembed")
	} else if d.Status != "superseded" {
		t.Fatalf("d1 status after reembed = %q, want superseded (JSONL should win)", d.Status)
	}
	if _, ok := byID[d2]; !ok {
		t.Fatal("d2 (JSONL-only) dropped by reembed = BUG-069 regression")
	}
}
