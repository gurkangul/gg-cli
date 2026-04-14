package outbox_test

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/gurkangul/gg-cli/internal/outbox"
)

func TestWriteListDelete(t *testing.T) {
	dir := t.TempDir()

	payload := map[string]string{"sha": "abc123", "lang": "go"}

	// Write two entries.
	id1, err := outbox.Write(dir, "full-index", payload)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	id2, err := outbox.Write(dir, "changed-index", payload)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if id1 == id2 {
		t.Fatal("expected distinct IDs")
	}

	// List should return both.
	entries, err := outbox.List(dir)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	// Verify payload round-trips.
	found := false
	for _, e := range entries {
		if e.ID == id1 {
			found = true
			if e.Kind != "full-index" {
				t.Errorf("kind: got %q, want %q", e.Kind, "full-index")
			}
			var got map[string]string
			if err := json.Unmarshal(e.Payload, &got); err != nil {
				t.Fatalf("unmarshal payload: %v", err)
			}
			if got["sha"] != "abc123" {
				t.Errorf("payload sha: got %q, want %q", got["sha"], "abc123")
			}
		}
	}
	if !found {
		t.Error("id1 not found in List output")
	}

	// Delete id1 — list should shrink to 1.
	if err := outbox.Delete(dir, id1); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	entries, err = outbox.List(dir)
	if err != nil {
		t.Fatalf("List after delete: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry after delete, got %d", len(entries))
	}
	if entries[0].ID != id2 {
		t.Errorf("expected remaining id2=%s, got %s", id2, entries[0].ID)
	}

	// Delete is idempotent.
	if err := outbox.Delete(dir, id1); err != nil {
		t.Fatalf("idempotent Delete: %v", err)
	}
}

func TestListEmptyDir(t *testing.T) {
	dir := t.TempDir()
	entries, err := outbox.List(dir)
	if err != nil {
		t.Fatalf("List empty dir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

func TestListMissingDir(t *testing.T) {
	dir := t.TempDir() + "/nonexistent"
	entries, err := outbox.List(dir)
	if err != nil {
		t.Fatalf("List missing dir: %v", err)
	}
	if entries != nil {
		t.Errorf("expected nil for missing dir, got %v", entries)
	}
}

func TestIncrementRetries(t *testing.T) {
	dir := t.TempDir()
	id, err := outbox.Write(dir, "full-index", "payload")
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := outbox.IncrementRetries(dir, id); err != nil {
		t.Fatalf("IncrementRetries: %v", err)
	}
	entries, _ := outbox.List(dir)
	if len(entries) != 1 || entries[0].Retries != 1 {
		t.Errorf("expected retries=1, got %+v", entries)
	}
}

func TestWriteCreatesDir(t *testing.T) {
	base := t.TempDir()
	dir := base + "/nested/ggdir"
	// dir doesn't exist yet — Write should create it.
	_, err := outbox.Write(dir, "full-index", "payload")
	if err != nil {
		t.Fatalf("Write with missing dir: %v", err)
	}
	if _, statErr := os.Stat(dir + "/outbox"); statErr != nil {
		t.Errorf("outbox subdir not created: %v", statErr)
	}
}

// TestConcurrentWriters verifies that N goroutines writing concurrently
// produce no corruption and the final entry count matches exactly.
// Runs with -race to detect data races in filesystem operations.
func TestConcurrentWriters(t *testing.T) {
	const writers = 32
	const perWriter = 100 // 3200 total entries
	dir := t.TempDir()

	var wg sync.WaitGroup
	ids := make([][]string, writers)
	errs := make([]error, writers)

	wg.Add(writers)
	for w := 0; w < writers; w++ {
		w := w
		go func() {
			defer wg.Done()
			localIDs := make([]string, 0, perWriter)
			payload := map[string]any{"writer": w, "data": "x"}
			for i := 0; i < perWriter; i++ {
				id, err := outbox.Write(dir, "stress", payload)
				if err != nil {
					errs[w] = err
					return
				}
				localIDs = append(localIDs, id)
			}
			ids[w] = localIDs
		}()
	}
	wg.Wait()

	for w, err := range errs {
		if err != nil {
			t.Errorf("writer %d failed: %v", w, err)
		}
	}

	entries, err := outbox.List(dir)
	if err != nil {
		t.Fatalf("List after stress: %v", err)
	}
	want := writers * perWriter
	if len(entries) != want {
		t.Errorf("entry count: got %d, want %d", len(entries), want)
	}

	// Verify every written ID is present and IDs are globally unique.
	seen := make(map[string]bool, want)
	for _, e := range entries {
		if seen[e.ID] {
			t.Errorf("duplicate entry ID %s", e.ID)
		}
		seen[e.ID] = true
	}
	for w, wids := range ids {
		for _, id := range wids {
			if !seen[id] {
				t.Errorf("writer %d: id %s not found in List output", w, id)
			}
		}
	}
}

// TestConcurrentWriteDelete exercises interleaved writes and deletes from
// multiple goroutines to confirm there are no race conditions or panics.
func TestConcurrentWriteDelete(t *testing.T) {
	const goroutines = 16
	const ops = 50
	dir := t.TempDir()

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < ops; i++ {
				id, err := outbox.Write(dir, "wd-stress", fmt.Sprintf("op-%d", i))
				if err != nil {
					return
				}
				// Immediately delete — simulates the "write then done" happy path.
				_ = outbox.Delete(dir, id)
			}
		}()
	}
	wg.Wait()

	// After all goroutines complete with immediate deletes, outbox should be empty.
	entries, err := outbox.List(dir)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected empty outbox, got %d entries", len(entries))
	}
}

// TestConvergenceLatency measures p50 and p99 Write+Delete round-trip latency
// as a proxy for outbox "convergence" time (the window where a crash would
// leave a dangling entry). This is a deterministic, in-process measurement —
// no Memgraph or Qdrant involved.
//
// Production-readiness gate (TASK-061): p99 must stay under 50ms on any
// developer machine. The check is skipped in CI short-mode (-short flag).
func TestConvergenceLatency(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping latency test in short mode")
	}

	const samples = 200
	dir := t.TempDir()
	payload := map[string]string{"sha": "deadbeef", "lang": "go"}
	latencies := make([]time.Duration, 0, samples)

	for i := 0; i < samples; i++ {
		start := time.Now()
		id, err := outbox.Write(dir, "latency-probe", payload)
		if err != nil {
			t.Fatalf("Write: %v", err)
		}
		if err := outbox.Delete(dir, id); err != nil {
			t.Fatalf("Delete: %v", err)
		}
		latencies = append(latencies, time.Since(start))
	}

	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	p50 := latencies[samples/2]
	p99 := latencies[int(float64(samples)*0.99)]

	t.Logf("Write+Delete latency (n=%d): p50=%v  p99=%v", samples, p50, p99)

	const maxP99 = 50 * time.Millisecond
	if p99 > maxP99 {
		t.Errorf("p99 latency %v exceeds gate %v — filesystem may be slow or test machine under load", p99, maxP99)
	}
}

// BenchmarkWrite measures raw Write throughput.
func BenchmarkWrite(b *testing.B) {
	dir := b.TempDir()
	payload := map[string]string{"sha": "abc", "lang": "go"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		id, err := outbox.Write(dir, "bench", payload)
		if err != nil {
			b.Fatal(err)
		}
		_ = id
	}
}

// BenchmarkWriteDelete measures the full Write→Delete round-trip throughput.
func BenchmarkWriteDelete(b *testing.B) {
	dir := b.TempDir()
	payload := map[string]string{"sha": "abc", "lang": "go"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		id, err := outbox.Write(dir, "bench", payload)
		if err != nil {
			b.Fatal(err)
		}
		if err := outbox.Delete(dir, id); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkList measures List throughput on a populated outbox.
func BenchmarkList(b *testing.B) {
	dir := b.TempDir()
	payload := map[string]string{"sha": "abc"}
	for i := 0; i < 100; i++ {
		if _, err := outbox.Write(dir, "seed", payload); err != nil {
			b.Fatal(err)
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := outbox.List(dir); err != nil {
			b.Fatal(err)
		}
	}
}
