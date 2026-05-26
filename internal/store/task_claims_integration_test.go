package store

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/gurkangul/gg-cli/internal/brain"
	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/qdrant/go-client/qdrant"
)

func TestTaskOwnershipIntegration(t *testing.T) {
	if os.Getenv("GG_INTEGRATION_TEST") != "1" {
		t.Skip("set GG_INTEGRATION_TEST=1 to run Qdrant integration tests")
	}
	projectID := fmt.Sprintf("test-task-claim-%d", time.Now().UTC().UnixNano())
	c, err := New(&config.QdrantConfig{Host: "127.0.0.1", Port: 6334}, t.TempDir(), projectID)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := c.HealthCheck(ctx); err != nil {
		t.Fatalf("Qdrant not reachable while GG_INTEGRATION_TEST=1: %v", err)
	}
	vectorCfg := qdrant.NewVectorsConfig(&qdrant.VectorParams{
		Size:     VectorSize,
		Distance: qdrant.Distance_Cosine,
	})
	if err := c.qc.CreateCollection(ctx, &qdrant.CreateCollection{
		CollectionName: c.collTasks(),
		VectorsConfig:  vectorCfg,
	}); err != nil {
		t.Skipf("Qdrant cannot create test collection: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_ = c.qc.DeleteCollection(cleanupCtx, c.collTasks())
	})

	id, err := c.CreateTask(ctx, Task{
		Title:  "ownership integration task",
		Status: "pending",
		Author: "test",
	}, make([]float32, VectorSize))
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	started, err := c.StartTask(ctx, id, "codex", 30*time.Minute)
	if err != nil {
		t.Fatalf("StartTask: %v", err)
	}
	if started.Status != "in_progress" {
		t.Fatalf("status: got %q", started.Status)
	}
	if started.Owner != "codex" {
		t.Fatalf("owner: got %q", started.Owner)
	}
	if started.LeaseUntil == "" {
		t.Fatal("expected lease_until")
	}
	if _, err := c.StartTask(ctx, id, "hermes", 30*time.Minute); err == nil {
		t.Fatal("expected second owner start to be rejected")
	}
	if _, err := c.RenewTask(ctx, id, "hermes", 30*time.Minute); err == nil {
		t.Fatal("expected non-owner renew to be rejected")
	}
	if _, err := c.ReleaseTask(ctx, id, "hermes"); err == nil {
		t.Fatal("expected non-owner release to be rejected")
	}
	renewed, err := c.RenewTask(ctx, id, "codex", 45*time.Minute)
	if err != nil {
		t.Fatalf("RenewTask owner: %v", err)
	}
	if renewed.Owner != "codex" || renewed.LeaseUntil == started.LeaseUntil {
		t.Fatalf("renewed = %+v, want codex with changed lease", renewed)
	}
	released, err := c.ReleaseTask(ctx, id, "codex")
	if err != nil {
		t.Fatalf("ReleaseTask owner: %v", err)
	}
	if released.Status != "pending" || released.Owner != "" || released.LeaseUntil != "" {
		t.Fatalf("released = %+v, want unowned pending task", released)
	}

	short, err := c.StartTask(ctx, id, "hermes", time.Second)
	if err != nil {
		t.Fatalf("StartTask hermes short lease: %v", err)
	}
	time.Sleep(1100 * time.Millisecond)
	taken, err := c.StartTask(ctx, id, "codex", 30*time.Minute)
	if err != nil {
		t.Fatalf("StartTask after expired lease: %v (short lease was %s)", err, short.LeaseUntil)
	}
	if taken.Owner != "codex" {
		t.Fatalf("expired lease takeover owner = %q, want codex", taken.Owner)
	}
}

func TestReadyForLivePlanUpdateIntegration(t *testing.T) {
	if os.Getenv("GG_INTEGRATION_TEST") != "1" {
		t.Skip("set GG_INTEGRATION_TEST=1 to run Qdrant integration tests")
	}
	projectID := fmt.Sprintf("test-task-ready-for-live-update-%d", time.Now().UTC().UnixNano())
	c, err := New(&config.QdrantConfig{Host: "127.0.0.1", Port: 6334}, t.TempDir(), projectID)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := c.HealthCheck(ctx); err != nil {
		t.Fatalf("Qdrant not reachable while GG_INTEGRATION_TEST=1: %v", err)
	}
	vectorCfg := qdrant.NewVectorsConfig(&qdrant.VectorParams{
		Size:     VectorSize,
		Distance: qdrant.Distance_Cosine,
	})
	if err := c.qc.CreateCollection(ctx, &qdrant.CreateCollection{
		CollectionName: c.collTasks(),
		VectorsConfig:  vectorCfg,
	}); err != nil {
		t.Skipf("Qdrant cannot create test collection: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_ = c.qc.DeleteCollection(cleanupCtx, c.collTasks())
	})

	id, err := c.CreateTask(ctx, Task{
		Title:  "ready for live integration task",
		Status: "pending",
		Author: "test",
	}, make([]float32, VectorSize))
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	if err := c.SetReadyForLive(ctx, id, "implementer", "plan A"); err != nil {
		t.Fatalf("SetReadyForLive(plan A): %v", err)
	}

	taskAfterPlanA, err := c.GetTask(ctx, id)
	if err != nil {
		t.Fatalf("GetTask(plan A): %v", err)
	}
	if taskAfterPlanA.Status != "ready_for_live" {
		t.Fatalf("status after plan A: got %q, want ready_for_live", taskAfterPlanA.Status)
	}
	if taskAfterPlanA.ReadyForLivePlan != "plan A" {
		t.Fatalf("ready_for_live_plan after plan A: got %q, want plan A", taskAfterPlanA.ReadyForLivePlan)
	}

	if err := c.SetReadyForLive(ctx, id, "implementer", "plan B"); err != nil {
		t.Fatalf("SetReadyForLive(plan B): %v", err)
	}

	taskAfterPlanB, err := c.GetTask(ctx, id)
	if err != nil {
		t.Fatalf("GetTask(plan B): %v", err)
	}
	if taskAfterPlanB.Status != "ready_for_live" {
		t.Fatalf("status after plan B: got %q, want ready_for_live", taskAfterPlanB.Status)
	}
	if taskAfterPlanB.ReadyForLivePlan != "plan B" {
		t.Fatalf("ready_for_live_plan after plan B: got %q, want plan B", taskAfterPlanB.ReadyForLivePlan)
	}

	events, _, err := brain.ReadAllWithCount(c.dataDir, taskEventsKind)
	if err != nil {
		t.Fatalf("ReadAllWithCount(task-events): %v", err)
	}
	actions := map[string]bool{}
	for _, e := range events {
		if e.Payload["task_id"] != id {
			continue
		}
		action, ok := e.Payload["action"].(string)
		if !ok {
			continue
		}
		actions[action] = true
	}
	if !actions["ready_for_live"] {
		t.Fatalf("expected ready_for_live event for %q", id)
	}
	if !actions["ready_for_live_updated"] {
		t.Fatalf("expected ready_for_live_updated event for %q", id)
	}
}
