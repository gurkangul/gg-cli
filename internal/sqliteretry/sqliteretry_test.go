package sqliteretry

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

// errBusy is a stand-in that reports the same "database is locked" text the
// modernc driver surfaces, so IsBusy's string fallback path is exercised without
// having to provoke a real lock from the (unexported-field) *sqlite.Error type.
var errBusy = errors.New("database is locked (5) (SQLITE_BUSY)")

func TestIsBusy(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"locked text", errors.New("database is locked"), true},
		{"table locked", errors.New("database table is locked: points"), true},
		{"busy token", errors.New("SQLITE_BUSY"), true},
		{"wrapped locked", fmt.Errorf("upsert into coll: %w", errBusy), true},
		{"unrelated", errors.New("no such column: foo"), false},
		{"constraint", errors.New("UNIQUE constraint failed"), false},
	}
	for _, tc := range cases {
		if got := IsBusy(tc.err); got != tc.want {
			t.Errorf("%s: IsBusy(%v) = %v, want %v", tc.name, tc.err, got, tc.want)
		}
	}
}

// TestDoRetriesThenSucceeds simulates a writer that is busy for the first two
// attempts and then succeeds — the helper must swallow the transient busy
// errors and return nil, having called fn exactly three times.
func TestDoRetriesThenSucceeds(t *testing.T) {
	calls := 0
	err := Do(context.Background(), func() error {
		calls++
		if calls < 3 {
			return errBusy
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Do returned %v, want nil after retry", err)
	}
	if calls != 3 {
		t.Fatalf("fn called %d times, want 3", calls)
	}
}

// TestDoGivesUpAfterMaxAttempts confirms a permanently-busy writer eventually
// fails (rather than looping forever) and that the original busy error is
// returned so the caller can still see what happened.
func TestDoGivesUpAfterMaxAttempts(t *testing.T) {
	calls := 0
	err := Do(context.Background(), func() error {
		calls++
		return errBusy
	})
	if !IsBusy(err) {
		t.Fatalf("Do returned %v, want a busy error after exhausting retries", err)
	}
	if calls != maxAttempts {
		t.Fatalf("fn called %d times, want %d (maxAttempts)", calls, maxAttempts)
	}
}

// TestDoNonBusyReturnsImmediately confirms a non-lock error is not retried.
func TestDoNonBusyReturnsImmediately(t *testing.T) {
	calls := 0
	want := errors.New("UNIQUE constraint failed")
	err := Do(context.Background(), func() error {
		calls++
		return want
	})
	if !errors.Is(err, want) {
		t.Fatalf("Do returned %v, want %v", err, want)
	}
	if calls != 1 {
		t.Fatalf("fn called %d times, want 1 (no retry on non-busy)", calls)
	}
}

// TestDoSuccessFirstTry confirms the happy path calls fn once.
func TestDoSuccessFirstTry(t *testing.T) {
	calls := 0
	if err := Do(context.Background(), func() error { calls++; return nil }); err != nil {
		t.Fatalf("Do returned %v, want nil", err)
	}
	if calls != 1 {
		t.Fatalf("fn called %d times, want 1", calls)
	}
}

// TestDoHonorsCanceledContext confirms a canceled context aborts the backoff
// wait instead of sleeping out the full schedule.
func TestDoHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	err := Do(ctx, func() error { return errBusy })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Do returned %v, want context.Canceled", err)
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("Do waited %v on a canceled context, want near-instant", elapsed)
	}
}

func TestBackoffSchedule(t *testing.T) {
	want := []time.Duration{50 * time.Millisecond, 100 * time.Millisecond, 200 * time.Millisecond, 400 * time.Millisecond, 800 * time.Millisecond}
	for i, w := range want {
		if got := backoff(i); got != w {
			t.Errorf("backoff(%d) = %v, want %v", i, got, w)
		}
	}
	// Beyond the schedule the delay is capped at maxDelay.
	if got := backoff(10); got != maxDelay {
		t.Errorf("backoff(10) = %v, want cap %v", got, maxDelay)
	}
}
