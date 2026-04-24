// Package spawn manages multi-agent orchestration sessions: liveness tracking,
// sequential queue state, and session registry. It provides the persistence
// layer that cmd/spawn_*.go commands write/read.
//
// File layout under ~/.gg/projects/<project_id>/spawn/:
//
//	heartbeat.json  — master session liveness (written by `gg spawn heartbeat`)
//	session.json    — current queue-runner state (written by `gg spawn queue`)
//	workers/        — one file per active worker pane (surface ID → task ID)
package spawn

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// HeartbeatFile is the name of the liveness file under the spawn dir.
const HeartbeatFile = "heartbeat.json"

// SessionFile is the name of the queue runner state file under the spawn dir.
const SessionFile = "session.json"

// WorkersDir is the subdirectory holding per-worker surface files.
const WorkersDir = "workers"

// StaleDuration is how long without a heartbeat before the master is
// considered dead. The master calls `gg spawn heartbeat` to reset this.
const StaleDuration = 5 * time.Minute

// Heartbeat records the last time the master session was seen alive.
type Heartbeat struct {
	// PID is the process ID of the master session (informational).
	PID int `json:"pid"`
	// Agent is the value of GG_AGENT at the time of the heartbeat.
	Agent string `json:"agent,omitempty"`
	// UpdatedAt is when this heartbeat was written.
	UpdatedAt time.Time `json:"updated_at"`
}

// QueueSession holds the persistent state of a sequential queue run.
type QueueSession struct {
	// Agent is the command used to launch worker panes (e.g. "claude").
	Agent string `json:"agent"`
	// StartedAt is when the queue run began.
	StartedAt time.Time `json:"started_at"`
	// UpdatedAt is the last time this file was written.
	UpdatedAt time.Time `json:"updated_at"`
	// Completed lists task IDs finished during this queue run.
	Completed []string `json:"completed"`
	// Skipped lists task IDs that were skipped (blocked/failed) this run.
	Skipped []string `json:"skipped"`
	// Current is the task ID currently being worked on (empty if idle).
	Current string `json:"current,omitempty"`
}

// WorkerEntry records a live worker pane.
type WorkerEntry struct {
	// SurfaceID is the opaque terminal pane handle.
	SurfaceID string `json:"surface_id"`
	// TaskID is the task the worker is handling.
	TaskID string `json:"task_id"`
	// Agent is the agent command running in the pane.
	Agent string `json:"agent"`
	// SpawnedAt is when the worker pane was created.
	SpawnedAt time.Time `json:"spawned_at"`
}

// Dir returns the spawn directory for the given runtime dir.
func Dir(runtimeDir string) string {
	return filepath.Join(runtimeDir, "spawn")
}

// ensureDir creates the spawn directory (and workers/ sub-dir) if absent.
func ensureDir(runtimeDir string) error {
	spawnDir := Dir(runtimeDir)
	if err := os.MkdirAll(filepath.Join(spawnDir, WorkersDir), 0o700); err != nil {
		return fmt.Errorf("create spawn dir: %w", err)
	}
	return nil
}

// WriteHeartbeat writes (or overwrites) the master heartbeat file.
func WriteHeartbeat(runtimeDir, agent string) error {
	if err := ensureDir(runtimeDir); err != nil {
		return err
	}
	hb := Heartbeat{
		PID:       os.Getpid(),
		Agent:     agent,
		UpdatedAt: time.Now().UTC(),
	}
	return writeJSON(filepath.Join(Dir(runtimeDir), HeartbeatFile), hb)
}

// ReadHeartbeat reads the master heartbeat file. Returns ErrNoHeartbeat when
// the file does not exist.
func ReadHeartbeat(runtimeDir string) (*Heartbeat, error) {
	path := filepath.Join(Dir(runtimeDir), HeartbeatFile)
	var hb Heartbeat
	if err := readJSON(path, &hb); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrNoHeartbeat
		}
		return nil, err
	}
	return &hb, nil
}

// IsMasterAlive reports whether the master heartbeat exists and was updated
// within StaleDuration. Returns false + reason string when stale or absent.
func IsMasterAlive(runtimeDir string) (bool, string) {
	hb, err := ReadHeartbeat(runtimeDir)
	if err != nil {
		if errors.Is(err, ErrNoHeartbeat) {
			return false, "no heartbeat recorded — master has not run `gg spawn heartbeat`"
		}
		return false, fmt.Sprintf("heartbeat read error: %v", err)
	}
	age := time.Since(hb.UpdatedAt)
	if age > StaleDuration {
		return false, fmt.Sprintf("master heartbeat is %.0fs old (stale after %.0fs) — master may be dead",
			age.Seconds(), StaleDuration.Seconds())
	}
	return true, ""
}

// WriteSession writes the queue runner session state.
func WriteSession(runtimeDir string, s *QueueSession) error {
	if err := ensureDir(runtimeDir); err != nil {
		return err
	}
	s.UpdatedAt = time.Now().UTC()
	return writeJSON(filepath.Join(Dir(runtimeDir), SessionFile), s)
}

// ReadSession reads the queue runner session state. Returns ErrNoSession when
// no session exists.
func ReadSession(runtimeDir string) (*QueueSession, error) {
	path := filepath.Join(Dir(runtimeDir), SessionFile)
	var s QueueSession
	if err := readJSON(path, &s); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrNoSession
		}
		return nil, err
	}
	return &s, nil
}

// RegisterWorker writes a worker entry to the workers/ dir.
func RegisterWorker(runtimeDir string, w WorkerEntry) error {
	if err := ensureDir(runtimeDir); err != nil {
		return err
	}
	// Use task ID as file name — one active worker per task is the invariant.
	name := sanitizeFilename(w.TaskID) + ".json"
	return writeJSON(filepath.Join(Dir(runtimeDir), WorkersDir, name), w)
}

// RemoveWorker deletes a worker entry (called after the worker pane exits).
func RemoveWorker(runtimeDir, taskID string) error {
	name := sanitizeFilename(taskID) + ".json"
	path := filepath.Join(Dir(runtimeDir), WorkersDir, name)
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// ListWorkers returns all registered worker entries.
func ListWorkers(runtimeDir string) ([]WorkerEntry, error) {
	dir := filepath.Join(Dir(runtimeDir), WorkersDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var workers []WorkerEntry
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		var w WorkerEntry
		if rErr := readJSON(filepath.Join(dir, e.Name()), &w); rErr == nil {
			workers = append(workers, w)
		}
	}
	return workers, nil
}

// ErrNoHeartbeat is returned when no heartbeat file exists.
var ErrNoHeartbeat = errors.New("no heartbeat file")

// ErrNoSession is returned when no session file exists.
var ErrNoSession = errors.New("no spawn session")

// sanitizeFilename replaces characters that are unsafe in filenames.
func sanitizeFilename(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			out = append(out, c)
		} else {
			out = append(out, '_')
		}
	}
	return string(out)
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	if err := os.WriteFile(path, append(b, '\n'), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}
