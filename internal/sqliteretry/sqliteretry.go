// Package sqliteretry wraps SQLite write execs with a small exponential-backoff
// retry so a transient SQLITE_BUSY / SQLITE_LOCKED (database is locked) does not
// surface as a hard error under many parallel gg agents (TASK-503).
//
// gg opens its embedded databases with WAL + busy_timeout + SetMaxOpenConns(1)
// (see internal/store and internal/graph). busy_timeout already makes a single
// statement wait for a held lock, but when several agents hammer the same
// project store a writer that is starved past the timeout still returns a hard
// "database is locked". This helper retries that specific class of error a few
// times with growing backoff before giving up — the same pattern the reference
// runWithRetry uses — so most lock conflicts resolve transparently.
//
// It is intentionally tiny, stdlib-only (plus the modernc.org/sqlite error type
// already in the dependency tree), CGO-free, and makes no network calls.
package sqliteretry

import (
	"context"
	"errors"
	"strings"
	"time"

	sqlite "modernc.org/sqlite"
)

// SQLite primary result codes for a held lock. Extended codes (e.g.
// SQLITE_BUSY_SNAPSHOT = 517) carry the primary in the low byte, so the check
// masks with 0xFF. Declared as literals to avoid importing the large,
// platform-specific modernc.org/sqlite/lib package.
const (
	codeBusy   = 5 // SQLITE_BUSY
	codeLocked = 6 // SQLITE_LOCKED
)

// Tunables for the backoff schedule. Defaults: 50/100/200/400ms (capped ~800ms)
// across a few attempts — long enough to ride out a transient writer stall,
// short enough that a genuinely stuck lock fails fast rather than hanging an
// interactive command.
const (
	maxAttempts = 5
	baseDelay   = 50 * time.Millisecond
	maxDelay    = 800 * time.Millisecond
)

// IsBusy reports whether err (or any error in its chain) is a transient SQLite
// lock contention (SQLITE_BUSY / SQLITE_LOCKED). It checks the typed
// *sqlite.Error code first (robust against message wording) and falls back to a
// string match for errors that have been flattened to plain text.
func IsBusy(err error) bool {
	if err == nil {
		return false
	}
	var se *sqlite.Error
	if errors.As(err, &se) {
		switch se.Code() & 0xFF {
		case codeBusy, codeLocked:
			return true
		}
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "database is locked") ||
		strings.Contains(msg, "database table is locked") ||
		strings.Contains(msg, "sqlite_busy") ||
		strings.Contains(msg, "sqlite_locked")
}

// Do runs fn, retrying on a transient SQLITE_BUSY / SQLITE_LOCKED with
// exponential backoff (baseDelay doubling each attempt, capped at maxDelay)
// before returning the last error. Any non-busy error returns immediately.
//
// fn must be the WRITE exec itself (e.g. a closure over db.ExecContext). It may
// be invoked more than once, so it must be idempotent — every gg write path
// that uses this is an INSERT ... ON CONFLICT, an idempotent UPDATE/DELETE, or
// runs inside a transaction that is rebuilt per attempt, so re-running is safe.
//
// The context is honored between attempts: if ctx is done while backing off, its
// error is returned instead of sleeping further.
func Do(ctx context.Context, fn func() error) error {
	var err error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		err = fn()
		if err == nil {
			return nil
		}
		if !IsBusy(err) {
			return err
		}
		if attempt == maxAttempts-1 {
			break
		}
		delay := backoff(attempt)
		if ctx != nil {
			t := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				t.Stop()
				return ctx.Err()
			case <-t.C:
			}
			continue
		}
		time.Sleep(delay)
	}
	return err
}

// backoff returns the delay before the (attempt+1)-th retry: baseDelay << attempt,
// capped at maxDelay (50 → 100 → 200 → 400 → 800ms …).
func backoff(attempt int) time.Duration {
	d := baseDelay << attempt
	if d > maxDelay || d <= 0 {
		return maxDelay
	}
	return d
}
