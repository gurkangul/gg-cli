package watchresilience

import (
	"errors"
	"testing"
	"time"
)

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func TestBreaker_OpensAfterThreshold(t *testing.T) {
	clk := &fakeClock{t: time.Unix(0, 0)}
	b := NewBreaker(3, 60*time.Second, clk.now)

	for i := 0; i < 2; i++ {
		if !b.Allow() {
			t.Fatalf("trigger %d should be allowed while closed", i)
		}
		b.RecordFailure(errors.New("boom"))
		if got := b.Snapshot().State; got != BreakerClosed {
			t.Fatalf("after %d failures state=%s, want closed", i+1, got)
		}
	}
	// Third consecutive failure trips the breaker.
	if !b.Allow() {
		t.Fatal("third trigger should still be allowed (closed)")
	}
	b.RecordFailure(errors.New("boom"))
	if got := b.Snapshot().State; got != BreakerOpen {
		t.Fatalf("after threshold failures state=%s, want open", got)
	}
	if b.Allow() {
		t.Fatal("open breaker must reject triggers within cooldown")
	}
}

func TestBreaker_HalfOpenProbeClosesOnSuccess(t *testing.T) {
	clk := &fakeClock{t: time.Unix(0, 0)}
	b := NewBreaker(1, 30*time.Second, clk.now)

	b.RecordFailure(errors.New("fail"))
	if b.Snapshot().State != BreakerOpen {
		t.Fatal("single failure with threshold=1 should open")
	}
	if b.Allow() {
		t.Fatal("should reject before cooldown elapses")
	}
	clk.advance(30 * time.Second)
	if !b.Allow() {
		t.Fatal("should admit one probe after cooldown (half-open)")
	}
	if b.Snapshot().State != BreakerHalfOpen {
		t.Fatalf("state=%s, want half-open after probe admitted", b.Snapshot().State)
	}
	if b.Allow() {
		t.Fatal("half-open admits only one probe at a time")
	}
	b.RecordSuccess()
	if b.Snapshot().State != BreakerClosed {
		t.Fatal("successful probe should close the circuit")
	}
	if !b.Allow() {
		t.Fatal("closed circuit should allow triggers")
	}
}

func TestBreaker_HalfOpenProbeReopensOnFailure(t *testing.T) {
	clk := &fakeClock{t: time.Unix(0, 0)}
	b := NewBreaker(1, 10*time.Second, clk.now)

	b.RecordFailure(errors.New("fail"))
	clk.advance(10 * time.Second)
	if !b.Allow() {
		t.Fatal("should admit probe after cooldown")
	}
	b.RecordFailure(errors.New("still broken"))
	if b.Snapshot().State != BreakerOpen {
		t.Fatal("failed probe should re-open the circuit")
	}
	if b.Allow() {
		t.Fatal("re-opened circuit must reject within fresh cooldown")
	}
}

func TestBreaker_ResetCloses(t *testing.T) {
	b := NewBreaker(1, time.Minute, nil)
	b.RecordFailure(errors.New("x"))
	b.Reset()
	if b.Snapshot().State != BreakerClosed || b.Snapshot().ConsecutiveFailures != 0 {
		t.Fatalf("reset should fully close: %+v", b.Snapshot())
	}
}

func TestBreaker_SnapshotCooldownRemaining(t *testing.T) {
	clk := &fakeClock{t: time.Unix(0, 0)}
	b := NewBreaker(1, 60*time.Second, clk.now)
	b.RecordFailure(errors.New("x"))
	clk.advance(20 * time.Second)
	if got := b.Snapshot().CooldownRemaining; got != 40*time.Second {
		t.Fatalf("cooldown remaining=%s, want 40s", got)
	}
	if msg := b.Snapshot().OpenError(); msg == "" {
		t.Fatal("open error message should be non-empty")
	}
}

func TestBreaker_DefaultsOnNonPositive(t *testing.T) {
	b := NewBreaker(0, 0, nil)
	// threshold defaults to 3.
	b.RecordFailure(errors.New("1"))
	b.RecordFailure(errors.New("2"))
	if b.Snapshot().State != BreakerClosed {
		t.Fatal("default threshold should be 3, not tripped after 2")
	}
	b.RecordFailure(errors.New("3"))
	if b.Snapshot().State != BreakerOpen {
		t.Fatal("should trip after 3 with defaults")
	}
}
