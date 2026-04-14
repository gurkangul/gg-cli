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
	if ggDir == "" || verb == "" || IsDisabled() {
		return
	}
	origin := originHuman
	switch {
	case strings.TrimSpace(os.Getenv("GG_ROLE")) != "":
		origin = originAgent
	case strings.TrimSpace(os.Getenv("GG_AGENT")) != "":
		origin = originAgent
	case strings.TrimSpace(fromFlag) != "":
		origin = originAgent
	}
	e := Entry{
		Verb:      verb,
		Origin:    origin,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	data, err := json.Marshal(e)
	if err != nil {
		return
	}
	f, err := os.OpenFile(filePath(ggDir), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	_, _ = f.Write(append(data, '\n'))
}

// WeeklySummary reads the telemetry log and returns a breakdown of verb calls
// in the last 7 days.
type WeeklySummary struct {
	Total       int            `json:"total"`
	AgentCalls  int            `json:"agent_calls"`
	HumanCalls  int            `json:"human_calls"`
	VerbCounts  map[string]int `json:"verb_counts"`
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
	}
	return sum, nil
}
