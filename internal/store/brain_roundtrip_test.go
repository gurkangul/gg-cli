package store_test

// Brain export/import round-trip integration test.
//
// This test requires a live Qdrant instance. It is skipped unless the
// environment variable GG_INTEGRATION_TEST=1 is set and Qdrant is reachable
// at the gRPC port (6334 on localhost).
//
// Usage:
//
//	GG_INTEGRATION_TEST=1 go test ./internal/store/ -run TestBrainRoundTrip -v -timeout 120s

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/qdrant/go-client/qdrant"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/scrub"
	"github.com/gurkangul/gg-cli/internal/store"
)

func TestBrainRoundTrip(t *testing.T) {
	if os.Getenv("GG_INTEGRATION_TEST") != "1" {
		t.Skip("set GG_INTEGRATION_TEST=1 to run brain round-trip integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	projectID := "test-brain-rt-" + uuid.New().String()[:8]
	ggDir := t.TempDir()

	cfg := &config.QdrantConfig{Host: "127.0.0.1", Port: 6334}
	c, err := store.New(cfg, ggDir, projectID)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	// ── Health check ───────────────────────────────────────────────────────
	hctx, hcancel := context.WithTimeout(ctx, 5*time.Second)
	defer hcancel()
	if err := c.HealthCheck(hctx); err != nil {
		t.Skipf("Qdrant not reachable: %v", err)
	}

	// ── Step 1: Ensure collections + seed data ─────────────────────────────
	if err := c.EnsureCollections(ctx, uint64(store.VectorSize)); err != nil {
		t.Fatalf("EnsureCollections: %v", err)
	}

	const (
		numDecisions = 10
		numTasks     = 5
		numMessages  = 20
	)

	// Seed decisions.
	decisionIDs := make([]string, numDecisions)
	for i := 0; i < numDecisions; i++ {
		id := uuid.New().String()
		decisionIDs[i] = id
		payload, _ := qdrant.TryValueMap(map[string]any{
			"text":       "test decision " + id,
			"reason":     "round-trip test",
			"tags":       []any{"test"},
			"created_at": time.Now().UTC().Format(time.RFC3339),
		})
		wait := true
		if upsertErr := seedPoint(ctx, c, "decisions", id, payload, wait); upsertErr != nil {
			t.Fatalf("seed decision: %v", upsertErr)
		}
	}

	// Seed tasks.
	taskIDs := make([]string, numTasks)
	for i := 0; i < numTasks; i++ {
		id := uuid.New().String()
		taskIDs[i] = id
		payload, _ := qdrant.TryValueMap(map[string]any{
			"title":      "test task " + id,
			"detail":     "round-trip",
			"status":     "pending",
			"created_at": time.Now().UTC().Format(time.RFC3339),
		})
		wait := true
		if upsertErr := seedPoint(ctx, c, "tasks", id, payload, wait); upsertErr != nil {
			t.Fatalf("seed task: %v", upsertErr)
		}
	}

	// Seed messages.
	for i := 0; i < numMessages; i++ {
		id := uuid.New().String()
		payload, _ := qdrant.TryValueMap(map[string]any{
			"content":    "test message " + id,
			"from":       "developer",
			"to":         "all",
			"created_at": time.Now().UTC().Format(time.RFC3339),
		})
		wait := true
		if upsertErr := seedPoint(ctx, c, "messages", id, payload, wait); upsertErr != nil {
			t.Fatalf("seed message: %v", upsertErr)
		}
	}

	totalSeeded := numDecisions + numTasks + numMessages

	// ── Step 2: Export to temp brain dir ───────────────────────────────────
	brainDir := filepath.Join(ggDir, "brain")
	partialDir := filepath.Join(ggDir, "brain.partial")
	if err := os.MkdirAll(partialDir, 0o755); err != nil {
		t.Fatalf("mkdir partial: %v", err)
	}

	checksums := make(map[string]string)
	counts := make(map[string]int)

	for _, kind := range store.BrainKind {
		records, scrollErr := c.ExportBrainCollection(ctx, kind)
		if scrollErr != nil {
			t.Fatalf("ExportBrainCollection %s: %v", kind, scrollErr)
		}
		fname := kind + ".jsonl"
		sum, n, writeErr := writeBrainJSONLTest(partialDir, fname, records)
		if writeErr != nil {
			t.Fatalf("write %s: %v", fname, writeErr)
		}
		checksums[fname] = sum
		counts[kind] = n
	}

	// Write empty chunks.jsonl and edges.jsonl.
	for _, fname := range []string{"chunks.jsonl", "edges.jsonl"} {
		if _, writeErr := os.Create(filepath.Join(partialDir, fname)); writeErr != nil {
			t.Fatalf("create %s: %v", fname, writeErr)
		}
		h := sha256.Sum256(nil)
		checksums[fname] = hex.EncodeToString(h[:])
		counts[strings.TrimSuffix(fname, ".jsonl")] = 0
	}

	manifest := map[string]any{
		"schema_version": 1,
		"gg_version":     "test",
		"project_id":     projectID,
		"exported_at":    time.Now().UTC().Truncate(time.Second).Format(time.RFC3339),
		"counts":         counts,
		"sha256":         checksums,
	}
	manifestBytes, _ := json.MarshalIndent(manifest, "", "  ")
	if err := os.WriteFile(filepath.Join(partialDir, "manifest.json"), append(manifestBytes, '\n'), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	if err := os.Rename(partialDir, brainDir); err != nil {
		t.Fatalf("rename partial→brain: %v", err)
	}

	t.Logf("Step 2: exported %d records to %s", totalSeeded, brainDir)

	// ── Step 3: Wipe Qdrant ────────────────────────────────────────────────
	if err := c.DropAllCollections(ctx); err != nil {
		t.Fatalf("DropAllCollections: %v", err)
	}
	if err := c.EnsureCollections(ctx, uint64(store.VectorSize)); err != nil {
		t.Fatalf("EnsureCollections after wipe: %v", err)
	}
	t.Log("Step 3: Qdrant wiped")

	// Verify it's empty.
	for _, kind := range store.BrainKind {
		after, _ := c.ExportBrainCollection(ctx, kind)
		if len(after) > 0 {
			t.Fatalf("after wipe %s still has %d records", kind, len(after))
		}
	}

	// ── Step 4: Import ─────────────────────────────────────────────────────
	for _, kind := range store.BrainKind {
		fname := filepath.Join(brainDir, kind+".jsonl")
		records, readErr := store.ReadBrainJSONL(fname)
		if readErr != nil {
			t.Fatalf("ReadBrainJSONL %s: %v", kind, readErr)
		}
		if upsertErr := c.UpsertBrainRecords(ctx, kind, records); upsertErr != nil {
			t.Fatalf("UpsertBrainRecords %s: %v", kind, upsertErr)
		}
	}
	t.Log("Step 4: import complete")

	// ── Step 5a: Assert point counts ───────────────────────────────────────
	for _, kind := range store.BrainKind {
		restored, scrollErr := c.ExportBrainCollection(ctx, kind)
		if scrollErr != nil {
			t.Fatalf("post-import scroll %s: %v", kind, scrollErr)
		}
		if got := len(restored); got != counts[kind] {
			t.Errorf("%s: restored %d, want %d", kind, got, counts[kind])
		}
	}
	t.Log("Step 5a: count assertions passed")

	// ── Step 5b: Spot-check 3 decision IDs are byte-equal ─────────────────
	spotIDs := decisionIDs
	if len(spotIDs) > 3 {
		spotIDs = spotIDs[:3]
	}

	exportedDecisions := loadJSONLByID(t, filepath.Join(brainDir, "decisions.jsonl"))
	restoredDecisions, _ := c.ExportBrainCollection(ctx, "decisions")
	restoredByID := make(map[string]store.BrainRecord, len(restoredDecisions))
	for _, r := range restoredDecisions {
		restoredByID[r.ID] = r
	}

	for _, id := range spotIDs {
		original, ok := exportedDecisions[id]
		if !ok {
			t.Errorf("spot-check: id %s not in export", id)
			continue
		}
		restored, ok := restoredByID[id]
		if !ok {
			t.Errorf("spot-check: id %s not in restored", id)
			continue
		}
		// Compare canonical JSON of payloads.
		origJSON, _ := store.CanonicalJSON(original)
		restJSON, _ := store.CanonicalJSON(restored.Payload)
		if !bytes.Equal(origJSON, restJSON) {
			t.Errorf("spot-check %s payload mismatch:\n  original: %s\n  restored: %s", id, origJSON, restJSON)
		}
	}
	t.Log("Step 5b: spot-check payload equality passed")

	// ── Step 6 (bonus): Second export must be byte-equal ───────────────────
	brainDir2 := filepath.Join(ggDir, "brain2")
	partialDir2 := filepath.Join(ggDir, "brain2.partial")
	if err := os.MkdirAll(partialDir2, 0o755); err != nil {
		t.Fatalf("mkdir partial2: %v", err)
	}
	checksums2 := make(map[string]string)
	counts2 := make(map[string]int)
	for _, kind := range store.BrainKind {
		records, _ := c.ExportBrainCollection(ctx, kind)
		fname := kind + ".jsonl"
		sum, n, _ := writeBrainJSONLTest(partialDir2, fname, records)
		checksums2[fname] = sum
		counts2[kind] = n
	}
	for _, fname := range []string{"chunks.jsonl", "edges.jsonl"} {
		_, _ = os.Create(filepath.Join(partialDir2, fname))
		h := sha256.Sum256(nil)
		checksums2[fname] = hex.EncodeToString(h[:])
	}
	_ = os.Rename(partialDir2, brainDir2)

	// Compare checksums from first export vs second.
	for fname, sum1 := range checksums {
		sum2, ok := checksums2[fname]
		if !ok {
			t.Errorf("round-trip: %s missing from second export", fname)
			continue
		}
		if fname == "manifest.json" {
			continue // exported_at timestamp differs
		}
		if sum1 != sum2 {
			t.Errorf("round-trip idempotence failure for %s:\n  export1: %s\n  export2: %s", fname, sum1, sum2)
		}
	}
	t.Log("Step 6: round-trip idempotence verified")

	// ── Cleanup ────────────────────────────────────────────────────────────
	_ = c.DropAllCollections(ctx)
}

// seedPoint upserts a single point with a deterministic zero-vector into the
// named collection. Qdrant requires a vector on create; the brain import path
// supports payload-only upserts onto existing points, but seeding a fresh
// collection needs a real vector. We use a zero-vector of the project's
// embedding dimension — deterministic and sufficient for round-trip assertions
// (content equality is checked via payload, not cosine similarity).
func seedPoint(ctx context.Context, c *store.Client, kind, id string, payload map[string]*qdrant.Value, wait bool) error {
	vec := make([]float32, store.VectorSize)
	return c.UpsertWithVector(ctx, kind, id, vec, payload, wait)
}

// writeBrainJSONLTest writes sorted BrainRecords as canonical JSONL to dir/filename.
// Returns (sha256hex, count, error).
func writeBrainJSONLTest(dir, filename string, records []store.BrainRecord) (string, int, error) {
	sort.Slice(records, func(i, j int) bool { return records[i].ID < records[j].ID })

	path := filepath.Join(dir, filename)
	f, err := os.Create(path) //nolint:gosec
	if err != nil {
		return "", 0, err
	}
	defer f.Close()

	h := sha256.New()
	w := bufio.NewWriter(f)
	for _, r := range records {
		obj := map[string]any{"id": r.ID, "payload": r.Payload}
		line, encErr := store.CanonicalJSON(obj)
		if encErr != nil {
			return "", 0, encErr
		}
		line = append(line, '\n')
		_, _ = w.Write(line)
		h.Write(line)
	}
	if err := w.Flush(); err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), len(records), nil
}

// TestBrainScrubberSmoke verifies that secret patterns are redacted when
// scrub.Any is applied to exported brain records, and that the raw secret
// does not appear in the resulting JSON — matching the behaviour of
// `gg brain export` (which calls scrub.Any on every record).
//
// Strict-mode behaviour: if scrubbed > 0, the CLI exits non-zero. This test
// directly validates that the scrub count is > 0, which is the condition that
// triggers the strict-mode error in cmd/brain.go.
func TestBrainScrubberSmoke(t *testing.T) {
	if os.Getenv("GG_INTEGRATION_TEST") != "1" {
		t.Skip("set GG_INTEGRATION_TEST=1 to run scrubber smoke integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	projectID := "test-scrub-" + uuid.New().String()[:8]
	ggDir := t.TempDir()

	cfg := &config.QdrantConfig{Host: "127.0.0.1", Port: 6334}
	c, err := store.New(cfg, ggDir, projectID)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	hctx, hcancel := context.WithTimeout(ctx, 5*time.Second)
	defer hcancel()
	if err := c.HealthCheck(hctx); err != nil {
		t.Skipf("Qdrant not reachable: %v", err)
	}

	if err := c.EnsureCollections(ctx, uint64(store.VectorSize)); err != nil {
		t.Fatalf("EnsureCollections: %v", err)
	}
	t.Cleanup(func() { _ = c.DropAllCollections(ctx) })

	const fakeSecret = "sk-testAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	pointID := uuid.New().String()
	payload := map[string]*qdrant.Value{
		"text":       {Kind: &qdrant.Value_StringValue{StringValue: "api key is " + fakeSecret}},
		"project_id": {Kind: &qdrant.Value_StringValue{StringValue: projectID}},
	}
	if err := seedPoint(ctx, c, "decisions", pointID, payload, true); err != nil {
		t.Fatalf("seedPoint: %v", err)
	}

	records, err := c.ExportBrainCollection(ctx, "decisions")
	if err != nil {
		t.Fatalf("ExportBrainCollection: %v", err)
	}

	var scrubCount int
	for i, r := range records {
		scrubbed, n := scrub.Any(r.Payload)
		if n > 0 {
			records[i].Payload = scrubbed.(map[string]any)
			scrubCount += n
		}
	}

	if scrubCount == 0 {
		t.Error("scrub.Any fired 0 times — expected at least 1 redaction")
	}

	raw, _ := json.Marshal(records)
	out := string(raw)
	if strings.Contains(out, fakeSecret) {
		t.Error("raw secret still present in scrubbed output")
	}
	if !strings.Contains(out, scrub.Redacted) {
		t.Errorf("expected %q in scrubbed output, not found", scrub.Redacted)
	}

	t.Logf("scrubber smoke: %d replacement(s); secret absent, [REDACTED] present ✓", scrubCount)
}

// loadJSONLByID reads a JSONL file and returns a map of id → payload.
func loadJSONLByID(t *testing.T, path string) map[string]map[string]any {
	t.Helper()
	data, err := os.ReadFile(path) //nolint:gosec
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	result := make(map[string]map[string]any)
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var r struct {
			ID      string         `json:"id"`
			Payload map[string]any `json:"payload"`
		}
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		result[r.ID] = r.Payload
	}
	return result
}
