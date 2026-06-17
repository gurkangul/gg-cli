package watchresilience

import "time"

// DefaultDebounce coalesces a burst of saves into one incremental reindex.
const DefaultDebounce = 2 * time.Second

// Debouncer coalesces a burst of per-file changes into a single reindex
// trigger. The foreground watch loop feeds it the set of changed files on
// every poll; the reindex is held back until the changed set has been quiet
// (no membership change) for the debounce window. This collapses a rapid run
// of saves — and saves spread across several files — into one incremental
// reindex of the whole changed set instead of one reindex per poll.
//
// Like Breaker, this is single-goroutine state driven by the foreground loop;
// it never schedules its own timers or goroutines.
type Debouncer struct {
	window   time.Duration
	now      func() time.Time
	lastSeen map[string]time.Time
	// settledAt is when the current changed set last changed membership; the
	// reindex fires once now()-settledAt >= window.
	settledAt time.Time
	pending   bool
}

// NewDebouncer builds a debouncer with the given quiescence window. A
// non-positive window falls back to DefaultDebounce.
func NewDebouncer(window time.Duration, now func() time.Time) *Debouncer {
	if window <= 0 {
		window = DefaultDebounce
	}
	if now == nil {
		now = time.Now
	}
	return &Debouncer{
		window:   window,
		now:      now,
		lastSeen: make(map[string]time.Time),
	}
}

// Observe records the currently changed file set seen on this poll and reports
// whether the burst has settled long enough to reindex. When it returns true
// the caller should run one incremental reindex of the changed set and then
// call Reset.
//
// Semantics:
//   - empty set: nothing pending, returns false.
//   - set membership changed since last poll (a new save landed): the
//     quiescence timer restarts, returns false (coalesce more).
//   - set unchanged for >= window: returns true (burst settled, reindex now).
func (d *Debouncer) Observe(files []string) bool {
	if len(files) == 0 {
		d.pending = false
		return false
	}
	now := d.now()
	changed := d.merge(files, now)
	if changed || !d.pending {
		d.settledAt = now
		d.pending = true
		return false
	}
	return now.Sub(d.settledAt) >= d.window
}

// merge updates lastSeen with the current set and reports whether membership
// changed (a file appeared or disappeared) since the previous observation.
func (d *Debouncer) merge(files []string, now time.Time) bool {
	cur := make(map[string]struct{}, len(files))
	for _, f := range files {
		cur[f] = struct{}{}
	}
	changed := false
	for f := range cur {
		if _, ok := d.lastSeen[f]; !ok {
			changed = true
		}
		d.lastSeen[f] = now
	}
	for f := range d.lastSeen {
		if _, ok := cur[f]; !ok {
			delete(d.lastSeen, f)
			changed = true
		}
	}
	return changed
}

// Pending reports whether a coalesced reindex is still being held back.
func (d *Debouncer) Pending() bool { return d.pending }

// Reset clears the coalesced set after a reindex has run.
func (d *Debouncer) Reset() {
	d.lastSeen = make(map[string]time.Time)
	d.settledAt = time.Time{}
	d.pending = false
}
