// session.go — session-level compact context aggregation (TASK-286).
//
// SummarizeSessions groups telemetry entries by SessionID and computes
// p50/p95 cumulative compact output bytes, enabling gg status to surface
// context-pressure signals across parallel agent sessions.
package telemetry

import (
	"encoding/json"
	"os"
	"strings"
	"time"
)

// SessionMetrics holds per-session compact context byte aggregates.
type SessionMetrics struct {
	// ID is the session identifier (CLAUDE_SESSION_ID or GG_SESSION_ID value).
	ID string
	// CompactCalls is the total number of --compact invocations in the session.
	CompactCalls int
	// CompactBytesOut is the total bytes actually rendered across all compact calls.
	CompactBytesOut int
	// CompactBytesDefault is the total bytes the non-compact renderer would have used.
	CompactBytesDefault int
}

// SessionSummary holds aggregate statistics across sessions observed in the
// telemetry window. It is computed by SummarizeSessions and used by gg status
// to show context-pressure signals (TASK-286).
type SessionSummary struct {
	// ActiveSessions is the number of distinct session IDs seen in the window.
	ActiveSessions int
	// AvgCompactCallsPerSession is the mean number of compact calls per session.
	AvgCompactCallsPerSession float64
	// P50CumulativeKB is the median cumulative compact output bytes per session (KB).
	P50CumulativeKB float64
	// P95CumulativeKB is the 95th-percentile cumulative compact output bytes per session (KB).
	P95CumulativeKB float64
	// OverThresholdCount is the number of sessions whose cumulative compact bytes exceeded
	// sessionThresholdBytes (signal that the agent's context window is filling fast).
	OverThresholdCount int
}

// sessionThresholdBytes is the cumulative compact-output threshold above which a
// session is flagged as high-context-pressure. 100 KB of compact output is a
// proxy for significant agent context consumption within a single session.
const sessionThresholdBytes = 100 * 1024

// SummarizeSessions reads the telemetry file and returns session-level compact
// context aggregates for all entries at or after since that carry a SessionID.
// Entries without a SessionID (human-CLI invocations) are ignored. Returns a
// zero-value SessionSummary (not an error) when the file doesn't exist or no
// session-tagged entries are found.
func SummarizeSessions(runtimeDir string, since time.Time) (*SessionSummary, error) {
	data, err := os.ReadFile(filePath(runtimeDir))
	if os.IsNotExist(err) {
		return &SessionSummary{}, nil
	}
	if err != nil {
		return nil, err
	}

	sessions := map[string]*SessionMetrics{}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var e Entry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue
		}
		if e.SessionID == "" || !e.Compact {
			continue
		}
		ts, err := time.Parse(time.RFC3339, e.Timestamp)
		if err != nil || ts.Before(since) {
			continue
		}
		m := sessions[e.SessionID]
		if m == nil {
			m = &SessionMetrics{ID: e.SessionID}
			sessions[e.SessionID] = m
		}
		m.CompactCalls++
		m.CompactBytesOut += e.BytesOut
		m.CompactBytesDefault += e.BytesDefault
	}

	n := len(sessions)
	if n == 0 {
		return &SessionSummary{}, nil
	}

	// Collect per-session compact output bytes for percentile computation.
	bytesSlice := make([]int, 0, n)
	totalCalls := 0
	overThreshold := 0
	for _, m := range sessions {
		bytesSlice = append(bytesSlice, m.CompactBytesOut)
		totalCalls += m.CompactCalls
		if m.CompactBytesOut > sessionThresholdBytes {
			overThreshold++
		}
	}
	sortInts(bytesSlice)

	p50 := float64(bytesSlice[p50idx(n)]) / 1024.0
	p95 := float64(bytesSlice[p95idx(n)]) / 1024.0

	return &SessionSummary{
		ActiveSessions:            n,
		AvgCompactCallsPerSession: float64(totalCalls) / float64(n),
		P50CumulativeKB:           p50,
		P95CumulativeKB:           p95,
		OverThresholdCount:        overThreshold,
	}, nil
}

// sortInts sorts a slice of ints in ascending order (insertion sort — small n).
func sortInts(a []int) {
	for i := 1; i < len(a); i++ {
		x := a[i]
		j := i - 1
		for j >= 0 && a[j] > x {
			a[j+1] = a[j]
			j--
		}
		a[j+1] = x
	}
}

func p50idx(n int) int {
	i := (n * 50) / 100
	if i >= n {
		i = n - 1
	}
	return i
}

func p95idx(n int) int {
	i := (n * 95) / 100
	if i >= n {
		i = n - 1
	}
	return i
}
