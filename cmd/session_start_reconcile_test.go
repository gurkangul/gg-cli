package cmd

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/outbox"
	"github.com/gurkangul/gg-cli/internal/store"
)

// stubReplay swaps runAutoReconcileReplay for the duration of a test, recording
// whether it was invoked and returning the supplied result.
func stubReplay(t *testing.T, drained int, err error) *bool {
	t.Helper()
	called := false
	orig := runAutoReconcileReplay
	runAutoReconcileReplay = func(_ context.Context, _ string, _ int) (int, error) {
		called = true
		return drained, err
	}
	t.Cleanup(func() { runAutoReconcileReplay = orig })
	return &called
}

func seedOutbox(t *testing.T, ggDir string) {
	t.Helper()
	if _, err := outbox.Write(ggDir, store.OutboxKindDecision, map[string]string{"uuid": "u1"}); err != nil {
		t.Fatalf("seed outbox: %v", err)
	}
}

func newReconcileCmd() *cobra.Command {
	c := &cobra.Command{}
	c.SetContext(context.Background())
	return c
}

// Empty outbox → no output and the replay seam is never invoked (zero-cost path).
func TestReconcileOutbox_EmptyIsSilent(t *testing.T) {
	setupGGDir(t)
	t.Setenv("GG_NO_AUTO_RECONCILE", "")
	called := stubReplay(t, 0, nil)

	var out, errBuf bytes.Buffer
	reconcileOutboxIfNeeded(newReconcileCmd(), &out, &errBuf)

	if *called {
		t.Fatal("replay must not run when outbox is empty")
	}
	if out.Len() != 0 || errBuf.Len() != 0 {
		t.Fatalf("expected no output, got stdout=%q stderr=%q", out.String(), errBuf.String())
	}
}

// Opt-out env → seam not invoked even with pending entries.
func TestReconcileOutbox_OptOutRespected(t *testing.T) {
	ggDir := setupGGDir(t)
	seedOutbox(t, ggDir)
	t.Setenv("GG_NO_AUTO_RECONCILE", "1")
	called := stubReplay(t, 1, nil)

	var out, errBuf bytes.Buffer
	reconcileOutboxIfNeeded(newReconcileCmd(), &out, &errBuf)

	if *called {
		t.Fatal("replay must not run when GG_NO_AUTO_RECONCILE=1")
	}
	if out.Len() != 0 || errBuf.Len() != 0 {
		t.Fatalf("expected no output when opted out, got stdout=%q stderr=%q", out.String(), errBuf.String())
	}
}

func TestReconcileOutbox_OptOutValues(t *testing.T) {
	for _, v := range []string{"1", "true", "on", "yes", "TRUE", "On"} {
		t.Setenv("GG_NO_AUTO_RECONCILE", v)
		if !autoReconcileDisabled() {
			t.Fatalf("expected disabled for %q", v)
		}
	}
	for _, v := range []string{"", "0", "off", "no", "garbage"} {
		t.Setenv("GG_NO_AUTO_RECONCILE", v)
		if autoReconcileDisabled() {
			t.Fatalf("expected enabled for %q", v)
		}
	}
}

// Pending entries + reachable store (drained N) → one-line success on stdout.
func TestReconcileOutbox_DrainedPrintsSuccess(t *testing.T) {
	ggDir := setupGGDir(t)
	seedOutbox(t, ggDir)
	t.Setenv("GG_NO_AUTO_RECONCILE", "")
	called := stubReplay(t, 2, nil)

	var out, errBuf bytes.Buffer
	reconcileOutboxIfNeeded(newReconcileCmd(), &out, &errBuf)

	if !*called {
		t.Fatal("replay must run when entries exist and not opted out")
	}
	if got := out.String(); got == "" || !bytes.Contains(out.Bytes(), []byte("auto-reconciled 2 pending")) {
		t.Fatalf("expected success line, got stdout=%q", got)
	}
	if errBuf.Len() != 0 {
		t.Fatalf("expected no stderr on success, got %q", errBuf.String())
	}
}

// Qdrant unreachable → deferred, no spam (silent on both streams).
func TestReconcileOutbox_StoreDownIsSilent(t *testing.T) {
	ggDir := setupGGDir(t)
	seedOutbox(t, ggDir)
	t.Setenv("GG_NO_AUTO_RECONCILE", "")
	stubReplay(t, 0, errAutoReconcileStoreDown)

	var out, errBuf bytes.Buffer
	reconcileOutboxIfNeeded(newReconcileCmd(), &out, &errBuf)

	if out.Len() != 0 || errBuf.Len() != 0 {
		t.Fatalf("expected silence when Qdrant down, got stdout=%q stderr=%q", out.String(), errBuf.String())
	}
}

// Genuine replay failure → non-fatal stderr warning pointing at the manual path.
func TestReconcileOutbox_FailureWarnsNonFatal(t *testing.T) {
	ggDir := setupGGDir(t)
	seedOutbox(t, ggDir)
	t.Setenv("GG_NO_AUTO_RECONCILE", "")
	stubReplay(t, 0, context.DeadlineExceeded)

	var out, errBuf bytes.Buffer
	reconcileOutboxIfNeeded(newReconcileCmd(), &out, &errBuf)

	if out.Len() != 0 {
		t.Fatalf("expected no stdout on failure, got %q", out.String())
	}
	if !bytes.Contains(errBuf.Bytes(), []byte("gg doctor --reconcile")) {
		t.Fatalf("expected non-fatal warning pointing to manual reconcile, got %q", errBuf.String())
	}
}

// A replay that runs past autoReconcileTimeout must produce the bounded-timeout
// warning, never hanging session-start.
func TestReconcileOutbox_TimeoutBounded(t *testing.T) {
	ggDir := setupGGDir(t)
	seedOutbox(t, ggDir)
	t.Setenv("GG_NO_AUTO_RECONCILE", "")

	orig := runAutoReconcileReplay
	runAutoReconcileReplay = func(ctx context.Context, _ string, _ int) (int, error) {
		<-ctx.Done() // honour the bounded context instead of sleeping the full window
		return 0, ctx.Err()
	}
	t.Cleanup(func() { runAutoReconcileReplay = orig })

	cmdCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := &cobra.Command{}
	c.SetContext(cmdCtx)

	var out, errBuf bytes.Buffer
	done := make(chan struct{})
	go func() {
		reconcileOutboxIfNeeded(c, &out, &errBuf)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(autoReconcileTimeout + 2*time.Second):
		t.Fatal("reconcileOutboxIfNeeded did not return within the bounded window")
	}

	if !bytes.Contains(errBuf.Bytes(), []byte("timeout")) {
		t.Fatalf("expected timeout warning, got stderr=%q", errBuf.String())
	}
}

// autoReconcileReplay (real, not stubbed) must gate on Qdrant reachability: the
// fixture points at a closed port, so it returns the deferred sentinel and never
// touches the outbox entry.
func TestAutoReconcileReplay_StoreDownDefers(t *testing.T) {
	ggDir := setupGGDir(t)
	seedOutbox(t, ggDir)

	drained, err := autoReconcileReplay(context.Background(), ggDir, 1)
	if drained != 0 {
		t.Fatalf("expected 0 drained when Qdrant down, got %d", drained)
	}
	if err != errAutoReconcileStoreDown {
		t.Fatalf("expected store-down sentinel, got %v", err)
	}
	// Entry must survive for the next reconcile pass.
	entries, _ := outbox.List(ggDir)
	if len(entries) != 1 {
		t.Fatalf("expected entry to survive a deferred replay, got %d", len(entries))
	}
}

// Guard: config.GGDir resolves in the fixture so the gating path is exercised
// (regression against a silent early-return that would mask the logic).
func TestAutoReconcile_GGDirResolves(t *testing.T) {
	setupGGDir(t)
	if _, err := config.GGDir(); err != nil {
		t.Fatalf("GGDir should resolve in fixture: %v", err)
	}
}
