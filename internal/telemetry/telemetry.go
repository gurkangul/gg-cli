// Package telemetry records per-call usage data for gg verbs — strictly
// LOCAL (no network), used by gg's own dogfood metrics (DISC-008).
//
// Each call appends a single JSON line to <ggDir>/telemetry.jsonl.
// The file is append-only and never truncated by gg itself; callers can
// rotate or delete it freely.
//
// Disable telemetry entirely with: export GG_TELEMETRY=0
//
// Origin heuristic: if GG_ROLE is set the call is assumed to come from an
// agent; otherwise it is treated as human-initiated. This is a best-effort
// classification — any agent that runs without GG_ROLE will count as human.
package telemetry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	fileName      = "telemetry.jsonl"
	originAgent   = "agent"
	originHuman   = "human"
)

// Entry is a single telemetry record.
type Entry struct {
	Verb      string `json:"verb"`
	Origin    string `json:"origin"` // "agent" or "human"
	Timestamp string `json:"ts"`     // RFC3339
	// Compact-mode measurement fields (omitted for non-compact calls).
	// BytesOut is what was actually printed; BytesDefault is what the
	// non-compact renderer would have produced for the same data.
	Compact      bool `json:"compact,omitempty"`
	BytesOut     int  `json:"bytes_out,omitempty"`
	BytesDefault int  `json:"bytes_default,omitempty"`
	// --with-context fields (omitted when flag is not used).
	WithContext       bool `json:"with_context,omitempty"`
	ContextBlockBytes int  `json:"context_block_bytes,omitempty"`
}

func filePath(ggDir string) string {
	return filepath.Join(ggDir, fileName)
}

// IsDisabled reports whether the user has opted out via GG_TELEMETRY=0/false/no/off.
// Telemetry is local-only, but users may still disable it for any reason
// (CI noise reduction, simple preference). Default: enabled.
func IsDisabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("GG_TELEMETRY"))) {
	case "0", "false", "no", "off":
		return true
	default:
		return false
	}
}

// Record appends one entry to the telemetry log. Errors are silently ignored
// so a full disk or missing directory never breaks the command being recorded.
// No-op if the user has disabled telemetry via GG_TELEMETRY=0.
//
// Origin classification heuristic (best-effort):
//   - GG_ROLE env set                                → agent
//   - GG_AGENT env set (e.g. "claude-code", "gsd")   → agent
//   - --from flag passed (any non-empty value)       → agent
//   - Otherwise                                       → human
//
// Pass fromFlag = "" if the calling command has no --from flag.
func Record(ggDir, verb, fromFlag string) {
	recordEntry(ggDir, Entry{
		Verb:      verb,
		Origin:    classify(fromFlag),
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
}

// RecordCompact appends a telemetry entry with byte-count measurements for a
// --compact invocation. bytesDefault is what the non-compact renderer would
// have produced on the same data — callers must render both to compute the
// baseline.
func RecordCompact(ggDir, verb, fromFlag string, bytesOut, bytesDefault int) {
	recordEntry(ggDir, Entry{
		Verb:         verb,
		Origin:       classify(fromFlag),
		Timestamp:    time.Now().UTC().Format(time.RFC3339),
		Compact:      true,
		BytesOut:     bytesOut,
		BytesDefault: bytesDefault,
	})
}

// RecordWithContext appends a telemetry entry for a --with-context invocation.
// contextBlockBytes is the size in bytes of the appended === Related Context === block.
func RecordWithContext(ggDir, verb, fromFlag string, contextBlockBytes int) {
	recordEntry(ggDir, Entry{
		Verb:              verb,
		Origin:            classify(fromFlag),
		Timestamp:         time.Now().UTC().Format(time.RFC3339),
		WithContext:       true,
		ContextBlockBytes: contextBlockBytes,
	})
}

func classify(fromFlag string) string {
	switch {
	case strings.TrimSpace(os.Getenv("GG_ROLE")) != "":
		return originAgent
	case strings.TrimSpace(os.Getenv("GG_AGENT")) != "":
		return originAgent
	case strings.TrimSpace(fromFlag) != "":
		return originAgent
	}
	return originHuman
}

func recordEntry(ggDir string, e Entry) {
	if ggDir == "" || e.Verb == "" || IsDisabled() {
		return
	}
	data, err := json.Marshal(e)
	if err != nil {
		return
	}
	f, err := os.OpenFile(filePath(ggDir), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	_, _ = f.Write(append(data, '\n'))
}

// WeeklySummary reads the telemetry log and returns a breakdown of verb calls
// in the last 7 days.
type WeeklySummary struct {
	Total      int            `json:"total"`
	AgentCalls int            `json:"agent_calls"`
	HumanCalls int            `json:"human_calls"`
	VerbCounts map[string]int `json:"verb_counts"`
	// CompactCalls counts entries with Compact=true. The two byte totals are
	// summed across those entries; subtracting gives total bytes saved.
	CompactCalls        int `json:"compact_calls"`
	CompactBytesOut     int `json:"compact_bytes_out"`
	CompactBytesDefault int `json:"compact_bytes_default"`
	// WithContextCalls counts gg get --with-context invocations.
	WithContextCalls      int `json:"with_context_calls"`
	WithContextBytesTotal int `json:"with_context_bytes_total"`
}

// Summarize reads the telemetry file and aggregates the last 7 days of data.
// Returns an empty summary (not an error) when the file doesn't exist.
func Summarize(ggDir string) (*WeeklySummary, error) {
	data, err := os.ReadFile(filePath(ggDir))
	if os.IsNotExist(err) {
		return &WeeklySummary{VerbCounts: map[string]int{}}, nil
	}
	if err != nil {
		return nil, err
	}

	cutoff := time.Now().UTC().AddDate(0, 0, -7)
	sum := &WeeklySummary{VerbCounts: map[string]int{}}

	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var e Entry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue
		}
		ts, err := time.Parse(time.RFC3339, e.Timestamp)
		if err != nil || ts.Before(cutoff) {
			continue
		}
		sum.Total++
		sum.VerbCounts[e.Verb]++
		if e.Origin == originAgent {
			sum.AgentCalls++
		} else {
			sum.HumanCalls++
		}
		if e.Compact {
			sum.CompactCalls++
			sum.CompactBytesOut += e.BytesOut
			sum.CompactBytesDefault += e.BytesDefault
		}
		if e.WithContext {
			sum.WithContextCalls++
			sum.WithContextBytesTotal += e.ContextBlockBytes
		}
	}
	return sum, nil
}
