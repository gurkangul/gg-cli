package terminal

import (
	"context"
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
