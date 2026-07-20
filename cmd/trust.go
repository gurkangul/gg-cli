package cmd

import (
	"strings"
	"time"

	"github.com/gurkangul/gg-cli/internal/store"
)

// trust.go — TASK-519 verification age as a decaying, DERIVED trust signal.
//
// Evidence was a binary: a decision either rendered "Evidence: ..." or the
// literal "[unverified]" (BUG-086). That treats a claim verified this morning
// and one verified eight months ago as equally solid, which is exactly the
// failure mode a long-lived memory has — the ledger keeps insisting something
// was "verified" long after the code it was verified against moved on.
//
// The tier is DERIVED, never stored, and it deliberately affects only how a
// decision READS and (as a last tie-break) how it RANKS. It never changes
// validity, never hides anything, and never mutates status. An agent must still
// see a stale decision; it just needs to know the check behind it is old.
//
// Age comes from CreatedAt because evidence can only be attached at record time
// today — there is no re-verify verb, so the creation date IS the verification
// date. If a re-verification path is added later it should write a verified_at
// and this function should prefer it; the tier logic itself would not change.
//
// Pins and durable-policy tags bypass decay entirely. A recorded convention
// ("commit messages must be conventional") is not a measurement that goes stale;
// aging it would train agents to ignore exactly the entries that must not rot.

const (
	// trustAgingAfter is when a verified claim starts reading as weaker. Chosen
	// to be roughly a development cycle: long enough that routine work does not
	// flap the label, short enough to catch a claim the codebase has outrun.
	trustAgingAfter = 60 * 24 * time.Hour
	// trustStaleAfter is when a verified claim should be re-checked before being
	// leaned on.
	trustStaleAfter = 180 * 24 * time.Hour
)

// Trust tiers, strongest first.
const (
	trustVerified   = "verified"
	trustAging      = "aging"
	trustStale      = "stale"
	trustUnverified = "unverified"
)

// decayExemptTags are tags marking durable rules rather than measurements.
// They match the set that already bypasses the recency window elsewhere in gg.
var decayExemptTags = map[string]bool{
	"constraint": true,
	"convention": true,
	"policy":     true,
	"canon":      true,
}

// trustExempt reports whether a decision is a durable rule that must not decay.
func trustExempt(d store.Decision) bool {
	if d.Pinned {
		return true
	}
	for _, t := range d.Tags {
		if decayExemptTags[strings.ToLower(strings.TrimSpace(t))] {
			return true
		}
	}
	return false
}

// trustTier returns the decision's derived trust tier as of now.
func trustTier(d store.Decision, now time.Time) string {
	if strings.TrimSpace(d.Evidence) == "" {
		return trustUnverified
	}
	if trustExempt(d) {
		return trustVerified
	}
	created, err := time.Parse(time.RFC3339, strings.TrimSpace(d.CreatedAt))
	if err != nil {
		// An unparseable date is not evidence of staleness; do not punish it.
		return trustVerified
	}
	switch age := now.Sub(created); {
	case age >= trustStaleAfter:
		return trustStale
	case age >= trustAgingAfter:
		return trustAging
	default:
		return trustVerified
	}
}

// trustLabel is the bracketed marker rendered next to a decision. It always
// states the tier, so "verified" can never be confused with "verified a long
// time ago".
func trustLabel(tier string) string {
	switch tier {
	case trustVerified:
		return "[verified]"
	case trustAging:
		return "[verified · aging]"
	case trustStale:
		return "[verified · stale — reverify]"
	default:
		return "[unverified]"
	}
}

// trustRankWeight orders tiers for use as a LAST tie-break in ranking. It is
// deliberately tiny in effect: it can only separate results that are already
// equal on every primary signal, so a fresh-but-irrelevant decision can never
// outrank a stale-but-exactly-matching one.
func trustRankWeight(tier string) int {
	switch tier {
	case trustVerified:
		return 3
	case trustAging:
		return 2
	case trustStale:
		return 1
	default:
		return 0
	}
}
