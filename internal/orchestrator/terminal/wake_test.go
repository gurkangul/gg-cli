package terminal

import (
	"context"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// IsAgentIdle
// ---------------------------------------------------------------------------

func TestIsAgentIdle_EmptyScreen(t *testing.T) {
	if !IsAgentIdle(nil) {
		t.Fatal("nil screen should be treated as idle")
	}
	if !IsAgentIdle([]byte{}) {
		t.Fatal("empty screen should be treated as idle")
	}
}

func TestIsAgentIdle_NoMarkers(t *testing.T) {
	screen := []byte("TASK-295 done — all tests pass\n> ")
	if !IsAgentIdle(screen) {
		t.Fatal("screen with no busy markers should be idle")
	}
}

func TestIsAgentIdle_SpinnerMarker(t *testing.T) {
	// Braille spinner rune in last line → active
	screen := []byte("Applying edit…\n⠹ tool call in progress\n")
	if IsAgentIdle(screen) {
		t.Fatal("screen with spinner rune should not be idle")
	}
}

func TestIsAgentIdle_ThinkingMarker(t *testing.T) {
	screen := []byte("Reading file…\nThinking...\n")
	if IsAgentIdle(screen) {
		t.Fatal("screen with 'thinking...' should not be idle")
	}
}

func TestIsAgentIdle_ThinkingMarkerCaseInsensitive(t *testing.T) {
	screen := []byte("THINKING…\n")
	if IsAgentIdle(screen) {
		t.Fatal("marker check must be case-insensitive")
	}
}

func TestIsAgentIdle_WorkingMarker(t *testing.T) {
	screen := []byte("Working...\n")
	if IsAgentIdle(screen) {
		t.Fatal("screen with 'working...' should not be idle")
	}
}

func TestIsAgentIdle_ToolCallMarker(t *testing.T) {
	screen := []byte("Executing tool call\n")
	if IsAgentIdle(screen) {
		t.Fatal("screen with 'tool call' should not be idle")
	}
}

func TestIsAgentIdle_BlankLinesBetweenContent(t *testing.T) {
	// Busy marker buried in lines above blank trailing lines → still active.
	screen := []byte("previous output\n⠧ running\n\n\n\n")
	if IsAgentIdle(screen) {
		t.Fatal("busy marker within last 5 non-blank lines should not be idle")
	}
}

func TestIsAgentIdle_MarkerBeyondScanWindow(t *testing.T) {
	// Busy marker is more than 5 non-blank lines from the end → treated as idle
	// (we only scan the tail, not the whole buffer).
	older := "⠋ thinking\nline1\nline2\nline3\nline4\nline5\nline6\n"
	screen := []byte(older)
	// The spinner is at position 0; lines 1-6 are the 5 most-recent non-blank
	// lines, so the spinner falls outside the window.
	if !IsAgentIdle(screen) {
		t.Fatal("busy marker >5 non-blank lines from bottom should be outside scan window")
	}
}

// ---------------------------------------------------------------------------
// IsAgentIdle — word-boundary anchoring for "running" / "executing"
// ---------------------------------------------------------------------------

func TestIsAgentIdle_RunningAsWholeWord(t *testing.T) {
	screen := []byte("running test suite\n")
	if IsAgentIdle(screen) {
		t.Fatal("standalone 'running' should be detected as busy")
	}
}

func TestIsAgentIdle_ExecutingAsWholeWord(t *testing.T) {
	screen := []byte("Executing pipeline step\n")
	if IsAgentIdle(screen) {
		t.Fatal("standalone 'Executing' should be detected as busy")
	}
}

func TestIsAgentIdle_RunningEmbeddedInWord(t *testing.T) {
	// "outrunning" contains "running" but is not a standalone word → idle.
	screen := []byte("benchmark outrunning baseline by 3x\n")
	if !IsAgentIdle(screen) {
		t.Fatal("'running' embedded inside 'outrunning' should not trigger busy")
	}
}

func TestIsAgentIdle_ExecutingEmbeddedInPath(t *testing.T) {
	// "test_executing.sh" contains "executing" mid-token → idle.
	screen := []byte("Loaded test_executing.sh\n")
	if !IsAgentIdle(screen) {
		t.Fatal("'executing' inside identifier 'test_executing.sh' should not trigger busy")
	}
}

func TestIsAgentIdle_RunningEmbeddedInHyphenatedIdentifier(t *testing.T) {
	// "task-running-tests" — hyphens are not \w; \b fires at the hyphen boundary.
	// "running" here is between hyphens so word-boundary anchoring DOES match it.
	// This is intentional: "task-running-tests" in agent output suggests activity.
	screen := []byte("task-running-tests: 3 passed\n")
	if IsAgentIdle(screen) {
		t.Fatal("'running' between hyphens should trigger busy (hyphen is a word boundary)")
	}
}

// ---------------------------------------------------------------------------
// WakeAndSend — idle path
// ---------------------------------------------------------------------------

func TestWakeAndSend_IdlePaneReceivesWakeSequence(t *testing.T) {
	f := NewFake()
	ctx := context.Background()
	id, _ := f.NewSplit(ctx, SplitOpts{})

	// Set idle screen content (no busy markers).
	f.SetScreen(id, []byte("All done.\n> "))

	start := time.Now()
	if err := WakeAndSend(ctx, f, id, "fix the gap"); err != nil {
		t.Fatalf("WakeAndSend returned error: %v", err)
	}
	elapsed := time.Since(start)

	// Idle path must have introduced the wakeDelay.
	if elapsed < wakeDelay {
		t.Fatalf("idle path should sleep at least %v, elapsed %v", wakeDelay, elapsed)
	}

	calls := filterCalls(f.Calls, id)
	// Expected sequence: ReadScreen, SendKey(enter) [wake], Send(text), SendKey(enter) [submit]
	assertCallSequence(t, calls, []Call{
		{Method: "ReadScreen", ID: id},
		{Method: "SendKey", ID: id, Arg: "enter"},
		{Method: "Send", ID: id, Arg: "fix the gap"},
		{Method: "SendKey", ID: id, Arg: "enter"},
	})
}

// ---------------------------------------------------------------------------
// WakeAndSend — active path
// ---------------------------------------------------------------------------

func TestWakeAndSend_ActivePaneSkipsWake(t *testing.T) {
	f := NewFake()
	ctx := context.Background()
	id, _ := f.NewSplit(ctx, SplitOpts{})

	// Set active screen content (spinner present).
	f.SetScreen(id, []byte("⠹ tool call in progress\n"))

	start := time.Now()
	if err := WakeAndSend(ctx, f, id, "Steering: fix gap"); err != nil {
		t.Fatalf("WakeAndSend returned error: %v", err)
	}
	elapsed := time.Since(start)

	// Active path must NOT sleep (no wakeDelay).
	if elapsed >= wakeDelay {
		t.Fatalf("active path should not sleep, elapsed %v", elapsed)
	}

	calls := filterCalls(f.Calls, id)
	// Expected: ReadScreen, Send(text), SendKey(enter) — NO wake Enter
	assertCallSequence(t, calls, []Call{
		{Method: "ReadScreen", ID: id},
		{Method: "Send", ID: id, Arg: "Steering: fix gap"},
		{Method: "SendKey", ID: id, Arg: "enter"},
	})
}

// ---------------------------------------------------------------------------
// WakeAndSend — screen-read failure falls back to idle assumption
// ---------------------------------------------------------------------------

func TestWakeAndSend_ReadScreenFailureFallsBackToIdlePath(t *testing.T) {
	// Use a backend that does not support ReadScreen (CanReadScreen=false).
	// WakeAndSend should assume idle and issue the wake sequence.
	noScreen := &noScreenTerminal{fake: NewFake()}
	ctx := context.Background()
	id, _ := noScreen.NewSplit(ctx, SplitOpts{})

	if err := WakeAndSend(ctx, noScreen, id, "hello"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	calls := filterCalls(noScreen.fake.Calls, id)
	// Should contain a wake SendKey before Send.
	foundWake := false
	for i, c := range calls {
		if c.Method == "SendKey" && c.Arg == "enter" {
			// Check that the next call is Send (not another SendKey → submit).
			if i+1 < len(calls) && calls[i+1].Method == "Send" {
				foundWake = true
				break
			}
		}
	}
	if !foundWake {
		t.Fatalf("expected wake SendKey before Send, calls: %v", calls)
	}
}

// ---------------------------------------------------------------------------
// WakeAndSend — per-surface lock (concurrent nudges must not interleave)
// ---------------------------------------------------------------------------

func TestWakeAndSend_ConcurrentNudgesSamePane_AreSerialised(t *testing.T) {
	// Two goroutines call WakeAndSend for the same surface concurrently.
	// Without the per-surface lock the wake-Enter from one caller can land
	// between the text-send and the submit-Enter of the other.  With the lock
	// each caller's (ReadScreen, wake-Enter?, Send, Enter) sequence must be
	// contiguous in the Calls log.
	//
	// We use an idle pane so both callers go through the wake path, making
	// interleaving maximally likely without the lock.

	// Reset the global lock map between sub-tests to avoid state leakage.
	surfaceLocks = &lockMap{}

	f := NewFake()
	ctx := context.Background()
	id, _ := f.NewSplit(ctx, SplitOpts{})
	f.SetScreen(id, []byte("idle\n> "))

	const goroutines = 10
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			_ = WakeAndSend(ctx, f, id, "msg")
		}()
	}
	wg.Wait()

	// Verify the call log: every Send("msg") must be immediately preceded by
	// a SendKey("enter") that is the wake Enter (not the submit Enter from a
	// different goroutine's sequence).  Concretely: search for each Send and
	// verify that the call two positions before it is a SendKey(enter) [wake]
	// and the call one position after it is a SendKey(enter) [submit].
	calls := filterCalls(f.Calls, id)
	for i, c := range calls {
		if c.Method != "Send" || c.Arg != "msg" {
			continue
		}
		// i-1 must be SendKey(enter) [wake]; i+1 must be SendKey(enter) [submit].
		if i < 2 {
			t.Errorf("Send at index %d has no room for preceding ReadScreen+wake", i)
			continue
		}
		wakeCall := calls[i-1]
		if wakeCall.Method != "SendKey" || wakeCall.Arg != "enter" {
			t.Errorf("call before Send[%d] is %+v, want SendKey(enter) [wake]", i, wakeCall)
		}
		if i+1 >= len(calls) {
			t.Errorf("Send at index %d has no following submit Enter", i)
			continue
		}
		submitCall := calls[i+1]
		if submitCall.Method != "SendKey" || submitCall.Arg != "enter" {
			t.Errorf("call after Send[%d] is %+v, want SendKey(enter) [submit]", i, submitCall)
		}
	}
}

func TestWakeAndSend_ConcurrentNudgesDifferentPanes_DoNotBlock(t *testing.T) {
	// Two goroutines targeting different surfaces must not block each other.
	surfaceLocks = &lockMap{}

	f := NewFake()
	ctx := context.Background()
	id1, _ := f.NewSplit(ctx, SplitOpts{})
	id2, _ := f.NewSplit(ctx, SplitOpts{})
	f.SetScreen(id1, []byte("idle\n"))
	f.SetScreen(id2, []byte("idle\n"))

	start := time.Now()
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_ = WakeAndSend(ctx, f, id1, "a")
	}()
	go func() {
		defer wg.Done()
		_ = WakeAndSend(ctx, f, id2, "b")
	}()
	wg.Wait()
	elapsed := time.Since(start)

	// Both calls go through the idle path (wakeDelay each).  If they ran
	// sequentially, elapsed ≥ 2*wakeDelay.  With separate locks they overlap
	// and elapsed should be < 2*wakeDelay.
	if elapsed >= 2*wakeDelay {
		t.Errorf("different-pane nudges appear to have serialised (elapsed %v ≥ 2×%v)", elapsed, wakeDelay)
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// filterCalls returns calls on the given surface ID, excluding the NewSplit.
func filterCalls(all []Call, id SurfaceID) []Call {
	var out []Call
	for _, c := range all {
		if c.Method == "NewSplit" {
			continue
		}
		if c.ID == id {
			out = append(out, c)
		}
	}
	return out
}

func assertCallSequence(t *testing.T, got []Call, want []Call) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("call count: want %d got %d\ncalls: %v", len(want), len(got), got)
	}
	for i, w := range want {
		g := got[i]
		if g.Method != w.Method || g.Arg != w.Arg {
			t.Errorf("call[%d]: want {%s %q} got {%s %q}", i, w.Method, w.Arg, g.Method, g.Arg)
		}
	}
}

// noScreenTerminal wraps FakeTerminal and reports CanReadScreen=false.
type noScreenTerminal struct {
	fake *FakeTerminal
}

func (n *noScreenTerminal) NewSplit(ctx context.Context, opts SplitOpts) (SurfaceID, error) {
	return n.fake.NewSplit(ctx, opts)
}
func (n *noScreenTerminal) Send(ctx context.Context, id SurfaceID, text string) error {
	return n.fake.Send(ctx, id, text)
}
func (n *noScreenTerminal) SendKey(ctx context.Context, id SurfaceID, key string) error {
	return n.fake.SendKey(ctx, id, key)
}
func (n *noScreenTerminal) ReadScreen(_ context.Context, _ SurfaceID) ([]byte, error) {
	return nil, ErrCapabilityUnsupported
}
func (n *noScreenTerminal) Focus(ctx context.Context, id SurfaceID) error {
	return n.fake.Focus(ctx, id)
}
func (n *noScreenTerminal) Close(ctx context.Context, id SurfaceID) error {
	return n.fake.Close(ctx, id)
}
func (n *noScreenTerminal) Capabilities() Caps {
	return Caps{CanReadScreen: false}
}
