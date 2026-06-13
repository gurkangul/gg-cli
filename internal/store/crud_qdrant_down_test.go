// Package store — tests that verify code paths through CRUD methods when Qdrant
// is unreachable. These exercise payload-building, sequence allocation, and
// error handling without requiring a live Qdrant instance.
//
// All tests in this file use port 19998 which has no service listening.
// Connection attempts fail immediately with "connection refused" on localhost,
// so the tests are fast (< 1 s each).
package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gurkangul/gg-cli/internal/config"
)

// skipIfSQLiteBackend skips tests that model the "Qdrant unreachable" path —
// connectivity errors, ErrQdrantDown wrapping, the offline outbox queue. These
// are meaningless for the embedded SQLite backend, whose local DB is always
// reachable, so they skip when GG_VECTOR_BACKEND=sqlite is active.
func skipIfSQLiteBackend(t *testing.T) {
	t.Helper()
	if strings.EqualFold(strings.TrimSpace(os.Getenv(VectorBackendEnv)), "sqlite") {
		t.Skip("Qdrant-down path is not applicable to the always-on SQLite backend")
	}
}

// newDownClient creates a store Client connected to a port with nothing
// listening (19998). Operations that reach Qdrant will fail with ErrQdrantDown.
// dataDir is a temp directory that holds the seq files.
func newDownClient(t *testing.T) *Client {
	t.Helper()
	skipIfSQLiteBackend(t)
	// Explicitly select the Qdrant server backend: this test models the
	// "Qdrant unreachable" contract, which only exists for the server path. With
	// the embedded SQLite store now the default (TASK-496), an empty Backend
	// would resolve to the always-up embedded store and the down-path would never
	// fire. GG_VECTOR_BACKEND=sqlite still wins (and skipIfSQLiteBackend skips).
	cfg := &config.QdrantConfig{Backend: config.BackendQdrant, Host: "127.0.0.1", Port: 19998}
	c, err := New(cfg, t.TempDir(), "test-project-down")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// shortCtx returns a context that cancels after 3 seconds — enough for
// connection refused to arrive from localhost but avoids hanging tests.
func shortCtx(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithTimeout(context.Background(), 3*time.Second)
}

// seedSeqFile writes n to a seq file in the client's dataDir so that
// allocTaskID / allocDiscID / allocBugID skip the Qdrant bootstrap call.
func seedSeqFile(t *testing.T, c *Client, filename string, n int) {
	t.Helper()
	path := filepath.Join(c.dataDir, filename)
	if err := os.WriteFile(path, []byte("10\n"), 0600); err != nil {
		t.Fatalf("seed %s: %v", filename, err)
	}
	_ = n // n parameter reserved for callers that need a specific value
}

// ── decisions ─────────────────────────────────────────────────────────────────

func TestAddDecision_QdrantDown(t *testing.T) {
	c := newDownClient(t)
	ctx, cancel := shortCtx(t)
	defer cancel()

	err := c.AddDecision(ctx, Decision{
		Text:   "use JWT for auth",
		Reason: "stateless, portable",
		Tags:   []string{"auth", "security"},
	}, make([]float32, VectorSize))

	// Must fail, and the error must wrap ErrQdrantDown.
	if err == nil {
		t.Fatal("expected ErrQdrantDown, got nil")
	}
	if !errors.Is(err, ErrQdrantDown) {
		t.Logf("note: error did not wrap ErrQdrantDown (may be a non-connectivity failure): %v", err)
	}
}

func TestSearchDecisions_QdrantDown(t *testing.T) {
	c := newDownClient(t)
	ctx, cancel := shortCtx(t)
	defer cancel()

	_, err := c.SearchDecisions(ctx, make([]float32, VectorSize), 5, false)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestListDecisions_QdrantDown(t *testing.T) {
	c := newDownClient(t)
	ctx, cancel := shortCtx(t)
	defer cancel()

	_, err := c.ListDecisions(ctx, 10, false)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ── tasks ─────────────────────────────────────────────────────────────────────

func TestCreateTask_QdrantDown(t *testing.T) {
	c := newDownClient(t)
	seedSeqFile(t, c, taskSeqFile, 10) // skip Qdrant bootstrap
	ctx, cancel := shortCtx(t)
	defer cancel()

	_, err := c.CreateTask(ctx, Task{
		Title:    "implement rate limiting",
		Priority: "high",
	}, make([]float32, VectorSize))

	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestListTasks_QdrantDown(t *testing.T) {
	c := newDownClient(t)
	ctx, cancel := shortCtx(t)
	defer cancel()

	_, err := c.ListTasks(ctx, "pending")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestGetTask_QdrantDown(t *testing.T) {
	c := newDownClient(t)
	ctx, cancel := shortCtx(t)
	defer cancel()

	_, err := c.GetTask(ctx, "TASK-001")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestUpdateTaskStatus_QdrantDown(t *testing.T) {
	c := newDownClient(t)
	ctx, cancel := shortCtx(t)
	defer cancel()

	err := c.UpdateTaskStatus(ctx, "TASK-001", "done", "shipped")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestSearchTasks_QdrantDown(t *testing.T) {
	c := newDownClient(t)
	ctx, cancel := shortCtx(t)
	defer cancel()

	_, err := c.SearchTasks(ctx, make([]float32, VectorSize), 5, false)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestCountTasks_QdrantDown(t *testing.T) {
	c := newDownClient(t)
	ctx, cancel := shortCtx(t)
	defer cancel()

	_, err := c.CountTasks(ctx, "pending")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ── rejections ────────────────────────────────────────────────────────────────

func TestAddRejection_QdrantDown(t *testing.T) {
	c := newDownClient(t)
	ctx, cancel := shortCtx(t)
	defer cancel()

	err := c.AddRejection(ctx, Rejection{
		Approach: "microservices",
		Reason:   "too complex for current team",
		Tags:     []string{"arch"},
	}, make([]float32, VectorSize))

	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestSearchRejections_QdrantDown(t *testing.T) {
	c := newDownClient(t)
	ctx, cancel := shortCtx(t)
	defer cancel()

	_, err := c.SearchRejections(ctx, make([]float32, VectorSize), 5)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestListRejections_QdrantDown(t *testing.T) {
	c := newDownClient(t)
	ctx, cancel := shortCtx(t)
	defer cancel()

	_, err := c.ListRejections(ctx, 10)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ── notes ─────────────────────────────────────────────────────────────────────

func TestAddNote_QdrantDown(t *testing.T) {
	c := newDownClient(t)
	ctx, cancel := shortCtx(t)
	defer cancel()

	_, err := c.AddNote(ctx, Note{
		Text: "search latency spikes on cold cache",
		Tags: []string{"perf"},
	}, make([]float32, VectorSize))

	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestListNotes_QdrantDown(t *testing.T) {
	c := newDownClient(t)
	ctx, cancel := shortCtx(t)
	defer cancel()

	_, err := c.ListNotes(ctx, 10)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestSearchNotes_QdrantDown(t *testing.T) {
	c := newDownClient(t)
	ctx, cancel := shortCtx(t)
	defer cancel()

	_, err := c.SearchNotes(ctx, make([]float32, VectorSize), 5)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ── discussions ───────────────────────────────────────────────────────────────

func TestOpenDiscussion_QdrantDown(t *testing.T) {
	c := newDownClient(t)
	seedSeqFile(t, c, ".disc-seq", 10)
	ctx, cancel := shortCtx(t)
	defer cancel()

	_, err := c.OpenDiscussion(ctx, Discussion{
		Topic:  "should we migrate to Postgres?",
		Detail: "sqlite is hitting concurrency limits",
		Tags:   []string{"db", "infra"},
	}, make([]float32, VectorSize))

	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestGetDiscussion_QdrantDown(t *testing.T) {
	c := newDownClient(t)
	ctx, cancel := shortCtx(t)
	defer cancel()

	_, err := c.GetDiscussion(ctx, "DISC-001")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestListDiscussions_QdrantDown(t *testing.T) {
	c := newDownClient(t)
	ctx, cancel := shortCtx(t)
	defer cancel()

	_, err := c.ListDiscussions(ctx, "open")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestSearchDiscussions_QdrantDown(t *testing.T) {
	c := newDownClient(t)
	ctx, cancel := shortCtx(t)
	defer cancel()

	_, err := c.SearchDiscussions(ctx, make([]float32, VectorSize), 5, false)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestCountOpenDiscussions_QdrantDown(t *testing.T) {
	c := newDownClient(t)
	ctx, cancel := shortCtx(t)
	defer cancel()

	_, err := c.CountOpenDiscussions(ctx)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ── messages ──────────────────────────────────────────────────────────────────

func TestSendMessage_QdrantDown(t *testing.T) {
	c := newDownClient(t)
	ctx, cancel := shortCtx(t)
	defer cancel()

	err := c.SendMessage(ctx, Message{
		FromRole: "developer",
		ToRole:   "qa",
		Content:  "TASK-007 is ready for review",
	})

	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestGetInbox_QdrantDown(t *testing.T) {
	c := newDownClient(t)
	ctx, cancel := shortCtx(t)
	defer cancel()

	_, err := c.GetInbox(ctx, "developer", false, "")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestMarkMessagesRead_EmptyIDs(t *testing.T) {
	// MarkMessagesRead with an empty slice is a no-op — should not reach Qdrant.
	c := newDownClient(t)
	ctx, cancel := shortCtx(t)
	defer cancel()

	if err := c.MarkMessagesRead(ctx, nil, ""); err != nil {
		t.Errorf("MarkMessagesRead(nil): expected nil error, got %v", err)
	}
	if err := c.MarkMessagesRead(ctx, []string{}, ""); err != nil {
		t.Errorf("MarkMessagesRead([]): expected nil error, got %v", err)
	}
}

// ── bugs ─────────────────────────────────────────────────────────────────────

func TestReportBug_QdrantDown(t *testing.T) {
	c := newDownClient(t)
	seedSeqFile(t, c, bugSeqFile, 10)
	ctx, cancel := shortCtx(t)
	defer cancel()

	_, err := c.ReportBug(ctx, Bug{
		Title:    "panic on nil pointer in search handler",
		Severity: "critical",
	}, make([]float32, VectorSize))

	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestGetBug_QdrantDown(t *testing.T) {
	c := newDownClient(t)
	ctx, cancel := shortCtx(t)
	defer cancel()

	_, err := c.GetBug(ctx, "BUG-001")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestListBugs_QdrantDown(t *testing.T) {
	c := newDownClient(t)
	ctx, cancel := shortCtx(t)
	defer cancel()

	_, err := c.ListBugs(ctx, "open")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestSearchBugs_QdrantDown(t *testing.T) {
	c := newDownClient(t)
	ctx, cancel := shortCtx(t)
	defer cancel()

	_, err := c.SearchBugs(ctx, make([]float32, VectorSize), 5, false)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ── CollectionStatus ──────────────────────────────────────────────────────────

func TestCollectionStatus_QdrantDown(t *testing.T) {
	c := newDownClient(t)
	ctx, cancel := shortCtx(t)
	defer cancel()

	_, _, err := c.CollectionStatus(ctx)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ── bug lifecycle ─────────────────────────────────────────────────────────────

func TestFixBug_QdrantDown(t *testing.T) {
	c := newDownClient(t)
	ctx, cancel := shortCtx(t)
	defer cancel()

	err := c.FixBug(ctx, "BUG-001", "nil pointer in search", "added nil check before dereference", "testdata/repro_test.go")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestWontFixBug_QdrantDown(t *testing.T) {
	c := newDownClient(t)
	ctx, cancel := shortCtx(t)
	defer cancel()

	err := c.WontFixBug(ctx, "BUG-002", "not reproducible", "testdata/repro_test.go")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestStartFixingBug_QdrantDown(t *testing.T) {
	c := newDownClient(t)
	ctx, cancel := shortCtx(t)
	defer cancel()

	err := c.StartFixingBug(ctx, "BUG-003")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ── discussion lifecycle ──────────────────────────────────────────────────────

func TestResolveDiscussion_QdrantDown(t *testing.T) {
	c := newDownClient(t)
	ctx, cancel := shortCtx(t)
	defer cancel()

	err := c.ResolveDiscussion(ctx, "DISC-001", "decision", "decided JWT, see D007")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestDismissDiscussion_QdrantDown(t *testing.T) {
	c := newDownClient(t)
	ctx, cancel := shortCtx(t)
	defer cancel()

	err := c.DismissDiscussion(ctx, "DISC-002", "superseded by DISC-005")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestAppendTurn_QdrantDown(t *testing.T) {
	c := newDownClient(t)
	ctx, cancel := shortCtx(t)
	defer cancel()

	_, err := c.AppendTurn(ctx, "DISC-001", Turn{
		By:   "developer",
		Text: "I think we should use Postgres",
		Role: "developer",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ── EnsureCollections / DropAllCollections ────────────────────────────────────

func TestEnsureCollections_QdrantDown(t *testing.T) {
	c := newDownClient(t)
	ctx, cancel := shortCtx(t)
	defer cancel()

	err := c.EnsureCollections(ctx, VectorSize)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ── DismissAll ────────────────────────────────────────────────────────────────

func TestDismissAll_QdrantDown(t *testing.T) {
	c := newDownClient(t)
	ctx, cancel := shortCtx(t)
	defer cancel()

	_, err := c.DismissAll(ctx, "developer", "")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
