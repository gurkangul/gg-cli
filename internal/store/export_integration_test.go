// Package store — integration tests for the export surface in export.go. These
// exercise the real Qdrant gRPC ScrollAndOffset paging API and are gated on
// GG_INTEGRATION_TEST=1 so the normal `go test ./...` gate stays green without a
// live Qdrant. Each test gets a unique throwaway project (collections dropped in
// t.Cleanup via newIntegrationClient).
//
//	GG_INTEGRATION_TEST=1 go test ./internal/store/ -run 'Export.*Integration' -v -count=1
package store

import (
	"testing"

	"github.com/google/uuid"
)

// TestExportScrollEmptyCollection_Integration verifies scrollWithVectors over a
// freshly-ensured (but empty) collection returns zero points and no error — i.e.
// an empty collection is empty, not an error and not a missing-collection nil.
func TestExportScrollEmptyCollection_Integration(t *testing.T) {
	c, ctx := newIntegrationClient(t, "export-empty")

	points, err := c.scrollWithVectors(ctx, c.collDecisions())
	if err != nil {
		t.Fatalf("scrollWithVectors over empty collection: %v", err)
	}
	if len(points) != 0 {
		t.Fatalf("empty collection scroll returned %d points, want 0", len(points))
	}
}

// TestExportScrollPopulatedCollection_Integration writes real decisions through
// AddDecision, then verifies scrollWithVectors reads them all back with payload
// and vector intact. This exercises ScrollAndOffset paging plus extractPayload /
// extractVector.
func TestExportScrollPopulatedCollection_Integration(t *testing.T) {
	c, ctx := newIntegrationClient(t, "export-pop")

	vec := make([]float32, VectorSize)
	vec[0] = 0.5
	vec[1] = 0.25
	ids := map[string]string{}
	for i := 0; i < 3; i++ {
		id := uuid.New().String()
		text := "export decision " + id
		if err := c.AddDecision(ctx, Decision{
			ID: id, Text: text, Reason: "r", Status: "active", Author: "tester",
		}, vec); err != nil {
			t.Fatalf("AddDecision: %v", err)
		}
		ids[id] = text
	}

	points, err := c.scrollWithVectors(ctx, c.collDecisions())
	if err != nil {
		t.Fatalf("scrollWithVectors over populated collection: %v", err)
	}
	if len(points) != len(ids) {
		t.Fatalf("scroll returned %d points, want %d", len(points), len(ids))
	}

	for _, p := range points {
		wantText, ok := ids[p.ID]
		if !ok {
			t.Fatalf("scroll returned unexpected point ID %q", p.ID)
		}
		// Payload extraction: the text field round-trips as a string.
		gotText, _ := p.Payload["text"].(string)
		if gotText != wantText {
			t.Fatalf("point %s payload text = %q, want %q", p.ID, gotText, wantText)
		}
		// Vector extraction: AddDecision upserts the supplied vector. Qdrant
		// stores cosine collections unit-normalized, so we assert the vector is
		// present, full-width, and preserves direction (ratio of the two
		// non-zero components) rather than exact magnitudes.
		if len(p.Vector) != VectorSize {
			t.Fatalf("point %s vector len = %d, want %d", p.ID, len(p.Vector), VectorSize)
		}
		if p.Vector[0] <= 0 || p.Vector[1] <= 0 {
			t.Fatalf("point %s vector head = [%v %v], want both positive (direction preserved)",
				p.ID, p.Vector[0], p.Vector[1])
		}
		wantRatio := vec[0] / vec[1]                 // 0.5 / 0.25 = 2.0
		gotRatio := p.Vector[0] / p.Vector[1]
		if diff := gotRatio - wantRatio; diff > 1e-4 || diff < -1e-4 {
			t.Fatalf("point %s vector component ratio = %v, want %v (direction not preserved)",
				p.ID, gotRatio, wantRatio)
		}
	}
}

// TestExportScrollCollectionNotFound_Integration verifies scrollWithVectors
// treats a missing collection as empty (nil, nil) rather than surfacing an error
// — the fresh-install case. We drop all collections first so the target is gone.
func TestExportScrollCollectionNotFound_Integration(t *testing.T) {
	c, ctx := newIntegrationClient(t, "export-missing")

	if err := c.DropAllCollections(ctx); err != nil {
		t.Fatalf("DropAllCollections: %v", err)
	}

	points, err := c.scrollWithVectors(ctx, c.collDecisions())
	if err != nil {
		t.Fatalf("scrollWithVectors over missing collection should be empty, got err: %v", err)
	}
	if points != nil {
		t.Fatalf("scroll over missing collection returned %v, want nil", points)
	}
}

// TestExportBundlePopulated_Integration verifies the public ExportBundle entry
// point: it scrolls every project collection and serializes records added across
// multiple kinds (decisions + bugs) into the bundle, keyed by kind suffix.
func TestExportBundlePopulated_Integration(t *testing.T) {
	c, ctx := newIntegrationClient(t, "export-bundle")

	vec := make([]float32, VectorSize)
	vec[0] = 1

	decID := uuid.New().String()
	if err := c.AddDecision(ctx, Decision{
		ID: decID, Text: "bundle decision", Reason: "r", Status: "active", Author: "tester",
	}, vec); err != nil {
		t.Fatalf("AddDecision: %v", err)
	}
	if _, err := c.ReportBug(ctx, Bug{
		Title: "bundle bug", Detail: "d", Severity: "low", By: "tester",
	}, vec); err != nil {
		t.Fatalf("ReportBug: %v", err)
	}

	bundle, err := c.ExportBundle(ctx)
	if err != nil {
		t.Fatalf("ExportBundle: %v", err)
	}
	if bundle.SourceProjectID != c.projectID {
		t.Fatalf("bundle source project = %q, want %q", bundle.SourceProjectID, c.projectID)
	}
	if got := len(bundle.Collections[collSuffixDecisions]); got != 1 {
		t.Fatalf("bundle decisions = %d, want 1", got)
	}
	if got := len(bundle.Collections[collSuffixBugs]); got != 1 {
		t.Fatalf("bundle bugs = %d, want 1", got)
	}
	// Collections with no records serialize as empty, not missing.
	if _, ok := bundle.Collections[collSuffixTasks]; !ok {
		t.Fatalf("bundle missing tasks key; collections=%v", keysOf(bundle.Collections))
	}
	if got := len(bundle.Collections[collSuffixTasks]); got != 0 {
		t.Fatalf("bundle tasks = %d, want 0 (none added)", got)
	}

	// The exported decision carries its payload text back.
	dec := bundle.Collections[collSuffixDecisions][0]
	if txt, _ := dec.Payload["text"].(string); txt != "bundle decision" {
		t.Fatalf("exported decision text = %q, want 'bundle decision'", txt)
	}
}

func keysOf(m map[string][]BundlePoint) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
