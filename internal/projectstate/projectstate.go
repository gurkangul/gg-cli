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
	"syscall"
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
	// RecentHydrations records targeted full-record reads that are safe to use as
	// a prerequisite for state-changing commands. Compact output is an index view;
	// these entries prove an agent fetched the full source record before acting.
	RecentHydrations []HydrationEntry `json:"recent_hydrations,omitempty"`
	// UpdatedAt is the RFC3339 timestamp of the last write.
	UpdatedAt string `json:"updated_at,omitempty"`
}

// HydrationEntry records one full-record fetch for an entity such as TASK-123.
type HydrationEntry struct {
	TS         string `json:"ts"`
	EntityType string `json:"entity_type"`
	EntityID   string `json:"entity_id"`
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
	// RationaleRecordID is the gg record UUID or display ID that was linked via
	// GG_BYPASS_RATIONALE_RECORD (TASK-318). Provides a queryable FK into the
	// brain so the bypass rationale is permanently retrievable via gg search.
	// Empty for entries recorded before TASK-318 or when only GG_BYPASS_RATIONALE was set.
	RationaleRecordID string `json:"rationale_record_id,omitempty"`
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
// UpdatedAt is always set to now. Multi-step read/modify/write callers should
// prefer Update so concurrent writers cannot clobber unrelated state fields.
func Write(runtimeDir string, s State) error {
	return writeUnlocked(runtimeDir, s)
}

func writeUnlocked(runtimeDir string, s State) error {
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

// Update serializes read-modify-write updates to state.json with a cooperative
// per-project file lock. This keeps high-frequency runtime writers (hydration
// proofs, bypass audit, session-start version deltas) from clobbering each
// other's fields in multi-agent Hermes sessions.
func Update(runtimeDir string, fn func(*State) error) error {
	return update(runtimeDir, false, fn)
}

func update(runtimeDir string, recoverReadError bool, fn func(*State) error) error {
	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", runtimeDir, err)
	}
	unlock, err := lock(runtimeDir)
	if err != nil {
		return err
	}
	defer unlock()

	s, err := Read(runtimeDir)
	if err != nil {
		if !recoverReadError {
			return err
		}
		s = State{}
	}
	if err := fn(&s); err != nil {
		return err
	}
	return writeUnlocked(runtimeDir, s)
}

func lock(runtimeDir string) (func(), error) {
	path := filepath.Join(runtimeDir, fileName+".lock")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open lock %s: %w", path, err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("lock %s: %w", path, err)
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}

// AppendBypass reads state.json, appends a BypassEntry with the current
// timestamp, and writes it back. Best-effort: runtimeDir missing or a
// concurrent writer clobbering our update surfaces as the returned error.
// Callers (gate paths) should treat this as advisory and never abort the
// user's command on write failure.
//
// rationale and rationaleTaskID come from GG_BYPASS_RATIONALE (TASK-317).
// rationaleRecordID comes from GG_BYPASS_RATIONALE_RECORD (TASK-318) —
// the gg record UUID that provides an auditable FK into the brain.
// Pass empty strings for legacy callers or when the field is not applicable.
func AppendBypass(runtimeDir, gate, taskID, actor, rationale, rationaleTaskID, rationaleRecordID string) error {
	return update(runtimeDir, true, func(s *State) error {
		s.BypassLog = append(s.BypassLog, BypassEntry{
			TS:                time.Now().UTC().Format(time.RFC3339),
			Gate:              gate,
			TaskID:            taskID,
			Actor:             actor,
			Rationale:         rationale,
			RationaleTaskID:   rationaleTaskID,
			RationaleRecordID: rationaleRecordID,
		})
		return nil
	})
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

// RecordHydration records that entityType/entityID was fetched in full. It is
// intentionally separate from telemetry's aggregate byte counters: task-close
// gates need a per-entity proof, not a project-wide hydration percentage.
func RecordHydration(runtimeDir, entityType, entityID string) error {
	return update(runtimeDir, true, func(s *State) error {
		now := time.Now().UTC()
		cutoff := now.Add(-24 * time.Hour)
		kept := s.RecentHydrations[:0]
		for _, h := range s.RecentHydrations {
			ts, tsErr := time.Parse(time.RFC3339, h.TS)
			if tsErr == nil && ts.After(cutoff) {
				kept = append(kept, h)
			}
		}
		s.RecentHydrations = append(kept, HydrationEntry{
			TS:         now.Format(time.RFC3339),
			EntityType: entityType,
			EntityID:   entityID,
		})
		return nil
	})
}

// HasRecentHydration reports whether entityType/entityID was fetched in full
// within the supplied duration. Malformed timestamps are ignored rather than
// treated as valid proof.
func HasRecentHydration(runtimeDir, entityType, entityID string, within time.Duration, now time.Time) (bool, HydrationEntry, error) {
	s, err := Read(runtimeDir)
	if err != nil {
		return false, HydrationEntry{}, err
	}
	cutoff := now.UTC().Add(-within)
	for _, h := range s.RecentHydrations {
		if h.EntityType != entityType || h.EntityID != entityID {
			continue
		}
		ts, tsErr := time.Parse(time.RFC3339, h.TS)
		if tsErr != nil {
			continue
		}
		if !ts.Before(cutoff) {
			return true, h, nil
		}
	}
	return false, HydrationEntry{}, nil
}
