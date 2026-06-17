// Package watchresilience provides the foreground-watch resilience primitives
// used by `gg index --watch`: a three-state circuit breaker and a per-file
// debounce coalescer.
//
// gg is no-daemon. These primitives never spawn goroutines that outlive the
// foreground command; they are pure in-memory state machines driven by the
// synchronous watch poll loop in cmd/index_watch.go. The reference design is
// the Wrongstack background-indexer (worker-thread async); here it is
// reimplemented as foreground, single-threaded Go.
package watchresilience

import (
	"fmt"
	"time"
)

// BreakerState is the three-state circuit-breaker lifecycle.
type BreakerState string

const (
	// BreakerClosed is normal operation; consecutive failures are counted.
	BreakerClosed BreakerState = "closed"
	// BreakerOpen rejects every reindex trigger for the cooldown after the
	// failure threshold is hit.
	BreakerOpen BreakerState = "open"
	// BreakerHalfOpen admits exactly one probe run after the cooldown elapses;
	// success closes the circuit, failure re-opens it.
	BreakerHalfOpen BreakerState = "half-open"
)

// DefaultFailureThreshold is the consecutive-failure count that opens the circuit.
const DefaultFailureThreshold = 3

// DefaultCooldown is how long an open circuit rejects triggers before a probe.
const DefaultCooldown = 60 * time.Second

// BreakerSnapshot is an immutable view of the breaker for status surfaces.
type BreakerSnapshot struct {
	State               BreakerState
	ConsecutiveFailures int
	LastFailure         string
	// CooldownRemaining is the time until an open circuit admits a probe (0
	// unless open).
	CooldownRemaining time.Duration
}

// Breaker is a three-state circuit breaker. It is NOT goroutine-safe: the
// foreground watch loop drives it from a single goroutine, matching gg's
// no-daemon contract. An injectable clock keeps it deterministic in tests.
type Breaker struct {
	failureThreshold int
	cooldown         time.Duration
	now              func() time.Time

	state               BreakerState
	consecutiveFailures int
	openedAt            time.Time
	lastFailure         string
	probeInFlight       bool
}

// NewBreaker builds a closed breaker. A non-positive threshold or cooldown
// falls back to the package defaults.
func NewBreaker(failureThreshold int, cooldown time.Duration, now func() time.Time) *Breaker {
	if failureThreshold <= 0 {
		failureThreshold = DefaultFailureThreshold
	}
	if cooldown <= 0 {
		cooldown = DefaultCooldown
	}
	if now == nil {
		now = time.Now
	}
	return &Breaker{
		failureThreshold: failureThreshold,
		cooldown:         cooldown,
		now:              now,
		state:            BreakerClosed,
	}
}

// Allow reports whether a reindex trigger may proceed. An open circuit
// transitions to half-open once the cooldown has elapsed, admitting exactly
// one probe; further triggers are rejected until that probe settles via
// RecordSuccess/RecordFailure.
func (b *Breaker) Allow() bool {
	switch b.state {
	case BreakerClosed:
		return true
	case BreakerOpen:
		if b.now().Sub(b.openedAt) < b.cooldown {
			return false
		}
		b.state = BreakerHalfOpen
		b.probeInFlight = true
		return true
	default: // half-open: admit only one probe at a time.
		if b.probeInFlight {
			return false
		}
		b.probeInFlight = true
		return true
	}
}

// RecordSuccess closes the circuit and clears the failure history.
func (b *Breaker) RecordSuccess() {
	b.state = BreakerClosed
	b.consecutiveFailures = 0
	b.lastFailure = ""
	b.probeInFlight = false
}

// RecordFailure records a genuine indexer failure (error or watchdog timeout).
// A half-open probe failure re-opens the circuit immediately; otherwise the
// circuit opens once the consecutive-failure threshold is reached. Callers must
// NOT feed transient/expected conditions (e.g. caller-initiated cancellation)
// here — only failures that should count against the breaker.
func (b *Breaker) RecordFailure(err error) {
	if err != nil {
		b.lastFailure = err.Error()
	} else {
		b.lastFailure = "unknown failure"
	}
	wasHalfOpen := b.state == BreakerHalfOpen
	b.probeInFlight = false
	b.consecutiveFailures++
	if wasHalfOpen || b.consecutiveFailures >= b.failureThreshold {
		b.state = BreakerOpen
		b.openedAt = b.now()
	}
}

// Reset force-closes the circuit (manual recovery: gg doctor --fix-index).
func (b *Breaker) Reset() {
	b.state = BreakerClosed
	b.consecutiveFailures = 0
	b.lastFailure = ""
	b.probeInFlight = false
	b.openedAt = time.Time{}
}

// Snapshot returns an immutable view for status reporting.
func (b *Breaker) Snapshot() BreakerSnapshot {
	var remaining time.Duration
	if b.state == BreakerOpen {
		remaining = b.cooldown - b.now().Sub(b.openedAt)
		if remaining < 0 {
			remaining = 0
		}
	}
	return BreakerSnapshot{
		State:               b.state,
		ConsecutiveFailures: b.consecutiveFailures,
		LastFailure:         b.lastFailure,
		CooldownRemaining:   remaining,
	}
}

// OpenError builds the human-facing message surfaced while the circuit is open.
func (s BreakerSnapshot) OpenError() string {
	msg := fmt.Sprintf("reindex paused after %d consecutive failures", s.ConsecutiveFailures)
	if s.LastFailure != "" {
		msg += fmt.Sprintf(" (last: %s)", s.LastFailure)
	}
	if s.CooldownRemaining > 0 {
		msg += fmt.Sprintf("; auto-retry in %s", s.CooldownRemaining.Round(time.Second))
	}
	msg += ". Recover now with gg doctor --fix-index."
	return msg
}
