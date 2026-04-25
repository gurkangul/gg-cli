// Package projectstate manages ~/.gg/projects/<id>/state.json — per-project
// runtime state that persists across sessions but is never committed to the
// repo. Currently tracks last_seen_cli_version so session-start can surface
// a version-delta notice when the CLI has been upgraded between sessions.
package projectstate

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const fileName = "state.json"

// State is the persisted per-project runtime state.
type State struct {
	// LastSeenCLIVersion is the CLI version string seen at the last session-start.
	// Empty string means "never recorded" (first session).
	LastSeenCLIVersion string `json:"last_seen_cli_version,omitempty"`
	// BypassLog is the append-only audit record of every GG_ENFORCEMENT=off
	// guard skip. Grows without bound by design — operators need the full
	// history to answer "how often are we bypassing, and for which tasks?".
	// Callers that need a bounded view should filter by time via
	// ListBypassesSince rather than truncating the slice.
	BypassLog []BypassEntry `json:"bypass_log,omitempty"`
	// UpdatedAt is the RFC3339 timestamp of the last write.
	UpdatedAt string `json:"updated_at,omitempty"`
}

// BypassEntry records a single enforcement-bypass event. Produced every time
// a gate (pre-task-done, gsd-guard, ready-for-live) decides to no-op because
// GG_ENFORCEMENT=off is set. Making these queryable is the precondition for
// a future bypass-rate pressure metric and for dogfood-audit retros.
type BypassEntry struct {
	// TS is the RFC3339 timestamp of the bypass.
	TS string `json:"ts"`
	// Gate is the name of the gate that was skipped (e.g. "pre-task-done",
	// "pre-task-done-ready-for-live", "gsd-guard").
	Gate string `json:"gate"`
	// TaskID is the task the bypass was observed on, when known. Empty for
	// gates that aren't task-scoped (e.g. gsd-guard in a planning call).
	TaskID string `json:"task_id,omitempty"`
	// Actor is the role/agent that triggered the bypass (GG_ROLE or GG_AGENT).
	// Empty when neither env var is set.
	Actor string `json:"actor,omitempty"`
	// Rationale is the value of GG_BYPASS_RATIONALE at bypass time (TASK-317).
	// Empty for legacy entries recorded before TASK-317 was enforced.
	Rationale string `json:"rationale,omitempty"`
	// RationaleTaskID is the TASK-NNN parsed from the rationale prefix, if any.
	// Populated when the rationale starts with "TASK-NNN:" or "TASK-NNN ".
	RationaleTaskID string `json:"rationale_task_id,omitempty"`
}

// Read loads the State from <runtimeDir>/state.json.
// Returns a zero State (not an error) when the file does not exist — first
// session, no prior version recorded.
func Read(runtimeDir string) (State, error) {
	path := filepath.Join(runtimeDir, fileName)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return State{}, nil
	}
	if err != nil {
		return State{}, fmt.Errorf("read %s: %w", path, err)
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return State{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return s, nil
}

// Write atomically persists the State to <runtimeDir>/state.json.
// UpdatedAt is always set to now.
func Write(runtimeDir string, s State) error {
	s.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(runtimeDir, fileName)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename %s → %s: %w", tmp, path, err)
	}
	return nil
}

// AppendBypass reads state.json, appends a BypassEntry with the current
// timestamp, and writes it back. Best-effort: runtimeDir missing or a
// concurrent writer clobbering our update surfaces as the returned error.
// Callers (gate paths) should treat this as advisory and never abort the
// user's command on write failure.
//
// rationale and rationaleTaskID come from GG_BYPASS_RATIONALE (TASK-317).
// Pass empty strings for legacy callers that predate the rationale requirement.
func AppendBypass(runtimeDir, gate, taskID, actor, rationale, rationaleTaskID string) error {
	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", runtimeDir, err)
	}
	s, err := Read(runtimeDir)
	if err != nil {
		// Don't refuse to log on a parse error — start a fresh state rather
		// than losing every subsequent bypass record to a single bad write.
		s = State{}
	}
	s.BypassLog = append(s.BypassLog, BypassEntry{
		TS:              time.Now().UTC().Format(time.RFC3339),
		Gate:            gate,
		TaskID:          taskID,
		Actor:           actor,
		Rationale:       rationale,
		RationaleTaskID: rationaleTaskID,
	})
	return Write(runtimeDir, s)
}

// ListBypassesSince returns every BypassEntry whose TS is after `since`.
// Entries with an unparseable TS are skipped silently — they're still in the
// underlying file for manual inspection, but shouldn't poison the count.
// Returned slice is freshly allocated; caller may mutate freely.
func ListBypassesSince(runtimeDir string, since time.Time) ([]BypassEntry, error) {
	s, err := Read(runtimeDir)
	if err != nil {
		return nil, err
	}
	var out []BypassEntry
	for _, e := range s.BypassLog {
		t, tErr := time.Parse(time.RFC3339, e.TS)
		if tErr != nil {
			continue
		}
		if t.After(since) {
			out = append(out, e)
		}
	}
	return out, nil
}
