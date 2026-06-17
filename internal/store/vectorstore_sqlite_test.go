package store

// Standalone correctness tests for the embedded SQLite vector store
// (TASK-493). These exercise the brute-force cosine search, the float32 codec,
// the payload-filter parity layer, the write path (upsert/overwrite/setpayload/
// delete), top-K/threshold limiting and on-disk persistence WITHOUT a the vector store
// server — so the embedded backend has its own safety net independent of the
// the vector store integration oracle.

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
)

func u64ptr(v uint64) *uint64   { return &v }
func f32ptr(v float32) *float32 { return &v }

func newTestSQLiteStore(t *testing.T, dir string) *sqliteStore {
	t.Helper()
	ss, err := newSQLiteStore(dir)
	if err != nil {
		t.Fatalf("newSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = ss.Close() })
	return ss
}

func createTestCollection(t *testing.T, ss *sqliteStore, name string, dim uint64) {
	t.Helper()
	if err := ss.CreateCollection(context.Background(), &CreateCollection{
		CollectionName: name,
		VectorsConfig:  NewVectorsConfig(&VectorParams{Size: dim, Distance: Distance_Cosine}),
	}); err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}
}

func upsertPoint(t *testing.T, ss *sqliteStore, coll, id string, payload map[string]*Value, vec ...float32) {
	t.Helper()
	if _, err := ss.Upsert(context.Background(), &UpsertPoints{
		CollectionName: coll,
		Points: []*PointStruct{{
			Id:      NewID(id),
			Vectors: NewVectors(vec...),
			Payload: payload,
		}},
	}); err != nil {
		t.Fatalf("Upsert %s: %v", id, err)
	}
}

func query(t *testing.T, ss *sqliteStore, req *QueryPoints) []*ScoredPoint {
	t.Helper()
	res, err := ss.Query(context.Background(), req)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	return res
}

// --- 1. cosineSimilarity: the math behind every ranking ---

func TestSQLiteVec_CosineSimilarity(t *testing.T) {
	cases := []struct {
		name string
		a, b []float32
		want float32
	}{
		{"identical", []float32{1, 0, 0}, []float32{1, 0, 0}, 1},
		{"orthogonal", []float32{1, 0, 0}, []float32{0, 1, 0}, 0},
		{"opposite", []float32{1, 0, 0}, []float32{-1, 0, 0}, -1},
		{"scale-invariant", []float32{1, 1, 0}, []float32{4, 4, 0}, 1},
		{"dim-mismatch", []float32{1, 0}, []float32{1, 0, 0}, 0},
		{"zero-vector", []float32{0, 0, 0}, []float32{1, 1, 1}, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := cosineSimilarity(c.a, c.b)
			if math.Abs(float64(got-c.want)) > 1e-6 {
				t.Errorf("cosine(%v,%v)=%v want %v", c.a, c.b, got, c.want)
			}
		})
	}
}

// --- 2. codec: float32 BLOB round-trip must be bit-exact ---

func TestSQLiteVec_CodecRoundTrip(t *testing.T) {
	in := []float32{0, 1, -1, 3.1415927, 1e-7, -2.5e6, 768.0, math.MaxFloat32}
	out := decodeVector(encodeVector(in))
	if len(out) != len(in) {
		t.Fatalf("len: got %d want %d", len(out), len(in))
	}
	for i := range in {
		if out[i] != in[i] {
			t.Errorf("idx %d: got %v want %v (not bit-exact)", i, out[i], in[i])
		}
	}
	if len(encodeVector([]float32{1})) != 4 {
		t.Errorf("each float32 must encode to 4 bytes")
	}
}

// --- 3. brute-force Query: ranking, descending scores, top-K ---

func TestSQLiteVec_QueryRanksByCosine(t *testing.T) {
	ss := newTestSQLiteStore(t, t.TempDir())
	const coll = "rank"
	createTestCollection(t, ss, coll, 3)
	idX := "11111111-1111-1111-1111-111111111111"
	idY := "22222222-2222-2222-2222-222222222222"
	idZ := "33333333-3333-3333-3333-333333333333"
	upsertPoint(t, ss, coll, idX, nil, 1, 0, 0)
	upsertPoint(t, ss, coll, idY, nil, 0, 1, 0)
	upsertPoint(t, ss, coll, idZ, nil, 0, 0, 1)

	res := query(t, ss, &QueryPoints{
		CollectionName: coll,
		Query:          NewQuery(0.9, 0.3, 0),
		Limit:          u64ptr(10),
	})
	if len(res) != 3 {
		t.Fatalf("want 3, got %d", len(res))
	}
	if res[0].GetId().GetUuid() != idX {
		t.Errorf("top should be X, got %s", res[0].GetId().GetUuid())
	}
	if res[1].GetId().GetUuid() != idY {
		t.Errorf("second should be Y, got %s", res[1].GetId().GetUuid())
	}
	for i := 1; i < len(res); i++ {
		if res[i-1].Score < res[i].Score {
			t.Errorf("scores not descending: %v then %v", res[i-1].Score, res[i].Score)
		}
	}
}

func TestSQLiteVec_QueryTopKLimit(t *testing.T) {
	ss := newTestSQLiteStore(t, t.TempDir())
	const coll = "limit"
	createTestCollection(t, ss, coll, 2)
	for i := 0; i < 5; i++ {
		upsertPoint(t, ss, coll, uuid.NewString(), nil, float32(i+1), 1)
	}
	res := query(t, ss, &QueryPoints{
		CollectionName: coll, Query: NewQuery(1, 1), Limit: u64ptr(2),
	})
	if len(res) != 2 {
		t.Errorf("limit=2 should return 2, got %d", len(res))
	}
}

func TestSQLiteVec_QueryScoreThreshold(t *testing.T) {
	ss := newTestSQLiteStore(t, t.TempDir())
	const coll = "thr"
	createTestCollection(t, ss, coll, 2)
	upsertPoint(t, ss, coll, uuid.NewString(), nil, 1, 0)  // cosine 1.0 vs query
	upsertPoint(t, ss, coll, uuid.NewString(), nil, -1, 0) // cosine -1.0 vs query
	res := query(t, ss, &QueryPoints{
		CollectionName: coll, Query: NewQuery(1, 0),
		Limit: u64ptr(10), ScoreThreshold: f32ptr(0.5),
	})
	if len(res) != 1 {
		t.Fatalf("threshold 0.5 should keep only the aligned point, got %d", len(res))
	}
}

// --- 4. payload filters: parity with the vector store's must / must_not / should ---

func TestSQLiteVec_PayloadFilters(t *testing.T) {
	ss := newTestSQLiteStore(t, t.TempDir())
	const coll = "filter"
	createTestCollection(t, ss, coll, 2)
	active := map[string]*Value{"status": NewValueString("active")}
	resolved := map[string]*Value{"status": NewValueString("resolved")}
	upsertPoint(t, ss, coll, "aaaaaaaa-0000-0000-0000-000000000001", active, 1, 0)
	upsertPoint(t, ss, coll, "aaaaaaaa-0000-0000-0000-000000000002", resolved, 1, 0)

	// Must: only active.
	res := query(t, ss, &QueryPoints{
		CollectionName: coll, Query: NewQuery(1, 0), Limit: u64ptr(10),
		Filter: &Filter{Must: []*Condition{NewMatchKeyword("status", "active")}},
	})
	if len(res) != 1 {
		t.Fatalf("Must status=active should return 1, got %d", len(res))
	}

	// MustNot: exclude resolved → only active.
	res = query(t, ss, &QueryPoints{
		CollectionName: coll, Query: NewQuery(1, 0), Limit: u64ptr(10),
		Filter: &Filter{MustNot: []*Condition{NewMatchKeyword("status", "resolved")}},
	})
	if len(res) != 1 {
		t.Fatalf("MustNot status=resolved should return 1, got %d", len(res))
	}

	// Should (OR): both statuses listed → both match.
	res = query(t, ss, &QueryPoints{
		CollectionName: coll, Query: NewQuery(1, 0), Limit: u64ptr(10),
		Filter: &Filter{Should: []*Condition{
			NewMatchKeyword("status", "active"),
			NewMatchKeyword("status", "resolved"),
		}},
	})
	if len(res) != 2 {
		t.Fatalf("Should(active|resolved) should return 2, got %d", len(res))
	}

	// nil filter matches everything.
	if !matchFilter(active, nil) {
		t.Error("nil filter must match all")
	}
}

// --- 5. write path: overwrite, setpayload, delete, count ---

func TestSQLiteVec_UpsertOverwrites(t *testing.T) {
	ss := newTestSQLiteStore(t, t.TempDir())
	const coll = "ov"
	createTestCollection(t, ss, coll, 2)
	id := "bbbbbbbb-0000-0000-0000-000000000001"
	upsertPoint(t, ss, coll, id, nil, 1, 0)
	upsertPoint(t, ss, coll, id, nil, 0, 1)
	n, err := ss.Count(context.Background(), &CountPoints{CollectionName: coll})
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if n != 1 {
		t.Errorf("overwrite should keep 1 point, got %d", n)
	}
}

func TestSQLiteVec_Delete(t *testing.T) {
	ss := newTestSQLiteStore(t, t.TempDir())
	const coll = "del"
	createTestCollection(t, ss, coll, 2)
	id := "cccccccc-0000-0000-0000-000000000001"
	upsertPoint(t, ss, coll, id, nil, 1, 0)
	if _, err := ss.Delete(context.Background(), &DeletePoints{
		CollectionName: coll,
		Points:         NewPointsSelector(NewID(id)),
	}); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	n, _ := ss.Count(context.Background(), &CountPoints{CollectionName: coll})
	if n != 0 {
		t.Errorf("after delete count should be 0, got %d", n)
	}
}

func TestSQLiteVec_SetPayload(t *testing.T) {
	ss := newTestSQLiteStore(t, t.TempDir())
	const coll = "sp"
	createTestCollection(t, ss, coll, 2)
	id := "dddddddd-0000-0000-0000-000000000001"
	upsertPoint(t, ss, coll, id, map[string]*Value{"status": NewValueString("active")}, 1, 0)
	if _, err := ss.SetPayload(context.Background(), &SetPayloadPoints{
		CollectionName: coll,
		Payload:        map[string]*Value{"status": NewValueString("resolved")},
		PointsSelector: NewPointsSelector(NewID(id)),
	}); err != nil {
		t.Fatalf("SetPayload: %v", err)
	}
	// After repointing status to resolved, a Must=active filter should exclude it.
	res := query(t, ss, &QueryPoints{
		CollectionName: coll, Query: NewQuery(1, 0), Limit: u64ptr(10),
		Filter: &Filter{Must: []*Condition{NewMatchKeyword("status", "active")}},
	})
	if len(res) != 0 {
		t.Errorf("SetPayload should have changed status to resolved; got %d active matches", len(res))
	}
}

// --- 6. collection lifecycle ---

func TestSQLiteVec_CollectionLifecycle(t *testing.T) {
	ss := newTestSQLiteStore(t, t.TempDir())
	createTestCollection(t, ss, "c1", 3)
	createTestCollection(t, ss, "c2", 3)
	cols, err := ss.ListCollections(context.Background())
	if err != nil {
		t.Fatalf("ListCollections: %v", err)
	}
	if len(cols) != 2 {
		t.Fatalf("want 2 collections, got %d (%v)", len(cols), cols)
	}
	if err := ss.DeleteCollection(context.Background(), "c1"); err != nil {
		t.Fatalf("DeleteCollection: %v", err)
	}
	cols, _ = ss.ListCollections(context.Background())
	if len(cols) != 1 || cols[0] != "c2" {
		t.Errorf("after delete want [c2], got %v", cols)
	}
}

// --- 7. persistence: the whole point of "files" — survive close + reopen ---

func TestSQLiteVec_PersistenceAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	const coll = "persist"
	id := "eeeeeeee-0000-0000-0000-000000000001"
	{
		ss, err := newSQLiteStore(dir)
		if err != nil {
			t.Fatalf("open1: %v", err)
		}
		createTestCollection(t, ss, coll, 3)
		upsertPoint(t, ss, coll, id, nil, 1, 2, 3)
		_ = ss.Close()
	}
	if _, err := os.Stat(filepath.Join(dir, "vectorstore.db")); err != nil {
		t.Fatalf("vectorstore.db must exist on disk: %v", err)
	}
	ss2 := newTestSQLiteStore(t, dir)
	res := query(t, ss2, &QueryPoints{
		CollectionName: coll, Query: NewQuery(1, 2, 3), Limit: u64ptr(5),
	})
	if len(res) != 1 || res[0].GetId().GetUuid() != id {
		t.Fatalf("data must survive reopen, got %+v", res)
	}
}

// --- 8. scale/latency sanity: brute-force at the documented project scale ---

func TestSQLiteVec_QueryLatencyAtScale(t *testing.T) {
	if testing.Short() {
		t.Skip("scale test")
	}
	if raceEnabled {
		t.Skip("latency gate is unreliable under -race instrumentation (10-20x overhead); correctness is covered by the other tests")
	}
	ss := newTestSQLiteStore(t, t.TempDir())
	const (
		coll = "scale"
		dim  = 768
		n    = 5000
	)
	createTestCollection(t, ss, coll, dim)
	mk := func(seed int) []float32 {
		v := make([]float32, dim)
		for i := 0; i < dim; i++ {
			v[i] = float32(math.Sin(float64(seed*31+i) * 0.001))
		}
		return v
	}
	pts := make([]*PointStruct, 0, n)
	for k := 0; k < n; k++ {
		pts = append(pts, &PointStruct{Id: NewID(uuid.NewString()), Vectors: NewVectors(mk(k)...)})
	}
	if _, err := ss.Upsert(context.Background(), &UpsertPoints{CollectionName: coll, Points: pts}); err != nil {
		t.Fatalf("bulk upsert: %v", err)
	}
	q := NewQuery(mk(1234)...)
	_ = query(t, ss, &QueryPoints{CollectionName: coll, Query: q, Limit: u64ptr(10)})
	const iters = 20
	start := time.Now()
	for i := 0; i < iters; i++ {
		if r := query(t, ss, &QueryPoints{CollectionName: coll, Query: q, Limit: u64ptr(10)}); len(r) != 10 {
			t.Fatalf("want top-10, got %d", len(r))
		}
	}
	per := time.Since(start) / iters
	t.Logf("brute-force top-10 over %d x %d-dim vectors: %v/query", n, dim, per.Round(time.Microsecond))
	if per > 200*time.Millisecond {
		t.Errorf("query latency %v exceeds 200ms budget", per)
	}
}
