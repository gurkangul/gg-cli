package watchresilience

import (
	"testing"
	"time"
)

func TestDebouncer_EmptySetNeverFires(t *testing.T) {
	clk := &fakeClock{t: time.Unix(0, 0)}
	d := NewDebouncer(2*time.Second, clk.now)
	if d.Observe(nil) {
		t.Fatal("empty set should not fire")
	}
	if d.Pending() {
		t.Fatal("empty observation should leave nothing pending")
	}
}

func TestDebouncer_SettledSetFiresAfterWindow(t *testing.T) {
	clk := &fakeClock{t: time.Unix(0, 0)}
	d := NewDebouncer(2*time.Second, clk.now)

	// First sighting starts the quiescence timer; must not fire yet.
	if d.Observe([]string{"a.go"}) {
		t.Fatal("first observation should start coalescing, not fire")
	}
	if !d.Pending() {
		t.Fatal("should be pending after first change")
	}
	// Same set, within window: still coalescing.
	clk.advance(1 * time.Second)
	if d.Observe([]string{"a.go"}) {
		t.Fatal("within window should keep coalescing")
	}
	// Same set, window elapsed: fire.
	clk.advance(2 * time.Second)
	if !d.Observe([]string{"a.go"}) {
		t.Fatal("settled set past window should fire")
	}
}

func TestDebouncer_NewSaveRestartsWindow(t *testing.T) {
	clk := &fakeClock{t: time.Unix(0, 0)}
	d := NewDebouncer(2*time.Second, clk.now)

	d.Observe([]string{"a.go"})
	clk.advance(1500 * time.Millisecond)
	// A new file lands in the burst — membership changed, restart the timer.
	if d.Observe([]string{"a.go", "b.go"}) {
		t.Fatal("a new save should restart the quiescence window, not fire")
	}
	clk.advance(1500 * time.Millisecond) // 1.5s since restart < 2s window
	if d.Observe([]string{"a.go", "b.go"}) {
		t.Fatal("should still be coalescing after the restart")
	}
	clk.advance(2 * time.Second)
	if !d.Observe([]string{"a.go", "b.go"}) {
		t.Fatal("burst should fire once it finally settles")
	}
}

func TestDebouncer_ResetClearsState(t *testing.T) {
	clk := &fakeClock{t: time.Unix(0, 0)}
	d := NewDebouncer(time.Second, clk.now)
	d.Observe([]string{"a.go"})
	d.Reset()
	if d.Pending() {
		t.Fatal("reset should clear pending state")
	}
	// After reset, the same file is treated as a fresh burst (restarts window).
	if d.Observe([]string{"a.go"}) {
		t.Fatal("post-reset observation should restart, not fire immediately")
	}
}

func TestDebouncer_RemovedFileRestartsWindow(t *testing.T) {
	clk := &fakeClock{t: time.Unix(0, 0)}
	d := NewDebouncer(time.Second, clk.now)
	d.Observe([]string{"a.go", "b.go"})
	clk.advance(2 * time.Second)
	// b.go reverted (no longer changed) — membership shrank, restart window.
	if d.Observe([]string{"a.go"}) {
		t.Fatal("shrinking the set should restart the window")
	}
	clk.advance(2 * time.Second)
	if !d.Observe([]string{"a.go"}) {
		t.Fatal("should fire once the smaller set settles")
	}
}
