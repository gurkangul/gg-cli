// Package spawn manages multi-agent orchestration sessions: liveness tracking,
// sequential queue state, and session registry. It provides the persistence
// layer that cmd/spawn_*.go commands write/read.
//
// File layout under ~/.gg/projects/<project_id>/spawn/:
//
//	heartbeat.json  — master session liveness (written by `gg spawn heartbeat`)
//	queue.json      — current queue-runner state (written by `gg spawn queue start`)
//	panes.json      — flat array of active worker panes (written by `gg spawn worker`)
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

// QueueFile is the name of the queue runner state file under the spawn dir.
const QueueFile = "queue.json"

// PanesFile is the flat JSON array of active worker panes under the spawn dir.
const PanesFile = "panes.json"

// LocksFile is the name of the advisory file-lock file under the spawn dir.
// Defined here (not in the locks package) so cmd/ can reference it without
// importing the locks package solely for the constant.
const LocksFile = "locks.json"

// DefaultMaxConcurrent is the default maximum number of simultaneous worker
// panes when GG_QUEUE_MAX is not set.
const DefaultMaxConcurrent = 3

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
	// Skipped lists task IDs permanently skipped (spawn error, repeated collision).
	Skipped []string `json:"skipped"`
	// SkippedTransient lists task IDs skipped due to a transient advisory
	// collision. On resume these are returned to the pending queue for retry.
	SkippedTransient []string `json:"skipped_transient,omitempty"`
	// CurrentTask is the task ID currently being worked on (empty if idle).
	CurrentTask string `json:"current_task,omitempty"`
	// Paused is set true when the queue is suspended via `gg spawn queue pause`.
	Paused bool `json:"paused,omitempty"`
}

// WorkerState represents the current lifecycle state of a worker pane.
type WorkerState string

const (
	// WorkerStateWorking means the worker is actively implementing its task.
	WorkerStateWorking WorkerState = "working"
	// WorkerStateIdle means the worker pane is open but no activity has been
	// detected for more than 5 minutes.
	WorkerStateIdle WorkerState = "idle"
	// WorkerStateWaiting means the worker sent a 'need-input' signal to the master.
	WorkerStateWaiting WorkerState = "waiting-on-master"
)

// WorkerPane records a live worker pane. Stored as elements of panes.json.
type WorkerPane struct {
	// SurfaceID is the opaque terminal pane handle.
	SurfaceID string `json:"surface_id"`
	// TaskID is the task the worker is handling.
	TaskID string `json:"task_id"`
	// Agent is the agent command running in the pane.
	Agent string `json:"agent"`
	// SpawnedAt is when the worker pane was created.
	SpawnedAt time.Time `json:"spawned_at"`
	// State is the current worker lifecycle state (working / idle / waiting-on-master).
	// Omitted (empty) in legacy entries written before TASK-276.
	State WorkerState `json:"state,omitempty"`
	// LastHeartbeat is the last time the worker's pane was seen alive via polling.
	// Zero value means no heartbeat has been recorded yet.
	LastHeartbeat time.Time `json:"last_heartbeat,omitempty"`
	// TouchedFiles is the set of file paths the worker has advisory-claimed
	// via 'gg task claim-files'. Populated from locks.json at dashboard render time.
	TouchedFiles []string `json:"touched_files,omitempty"`
}

// Dir returns the spawn directory for the given runtime dir.
func Dir(runtimeDir string) string {
	return filepath.Join(runtimeDir, "spawn")
}

// ensureDir creates the spawn directory if absent.
func ensureDir(runtimeDir string) error {
	if err := os.MkdirAll(Dir(runtimeDir), 0o700); err != nil {
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

// WriteQueue writes the queue runner session state to queue.json.
func WriteQueue(runtimeDir string, s *QueueSession) error {
	if err := ensureDir(runtimeDir); err != nil {
		return err
	}
	s.UpdatedAt = time.Now().UTC()
	return writeJSON(filepath.Join(Dir(runtimeDir), QueueFile), s)
}

// ReadQueue reads the queue runner session state. Returns ErrNoQueue when
// no queue.json exists.
func ReadQueue(runtimeDir string) (*QueueSession, error) {
	path := filepath.Join(Dir(runtimeDir), QueueFile)
	var s QueueSession
	if err := readJSON(path, &s); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrNoQueue
		}
		return nil, err
	}
	return &s, nil
}

// DeleteQueue removes queue.json (called on queue cancel).
func DeleteQueue(runtimeDir string) error {
	path := filepath.Join(Dir(runtimeDir), QueueFile)
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// readPanes reads panes.json, returning an empty slice if the file is absent.
func readPanes(runtimeDir string) ([]WorkerPane, error) {
	path := filepath.Join(Dir(runtimeDir), PanesFile)
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var panes []WorkerPane
	if err := json.Unmarshal(b, &panes); err != nil {
		return nil, fmt.Errorf("parse panes.json: %w", err)
	}
	return panes, nil
}

// writePanes writes panes.json.
func writePanes(runtimeDir string, panes []WorkerPane) error {
	if err := ensureDir(runtimeDir); err != nil {
		return err
	}
	b, err := json.MarshalIndent(panes, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal panes: %w", err)
	}
	path := filepath.Join(Dir(runtimeDir), PanesFile)
	if err := os.WriteFile(path, append(b, '\n'), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// RegisterPane adds a worker pane to panes.json.
func RegisterPane(runtimeDir string, w WorkerPane) error {
	panes, err := readPanes(runtimeDir)
	if err != nil {
		return err
	}
	// Remove any stale entry for the same task ID before adding.
	filtered := panes[:0]
	for _, p := range panes {
		if p.TaskID != w.TaskID {
			filtered = append(filtered, p)
		}
	}
	filtered = append(filtered, w)
	return writePanes(runtimeDir, filtered)
}

// RemovePane removes a worker pane from panes.json by task ID.
func RemovePane(runtimeDir, taskID string) error {
	panes, err := readPanes(runtimeDir)
	if err != nil {
		return err
	}
	filtered := panes[:0]
	for _, p := range panes {
		if p.TaskID != taskID {
			filtered = append(filtered, p)
		}
	}
	return writePanes(runtimeDir, filtered)
}

// ListPanes returns all registered worker panes from panes.json.
func ListPanes(runtimeDir string) ([]WorkerPane, error) {
	return readPanes(runtimeDir)
}

// UpdateWorkerState sets the State field on the pane with the given task ID.
// No-op if the pane is not found.
func UpdateWorkerState(runtimeDir, taskID string, state WorkerState) error {
	panes, err := readPanes(runtimeDir)
	if err != nil {
		return err
	}
	changed := false
	for i := range panes {
		if panes[i].TaskID == taskID {
			panes[i].State = state
			changed = true
			break
		}
	}
	if !changed {
		return nil
	}
	return writePanes(runtimeDir, panes)
}

// UpdateWorkerHeartbeat records the current time as LastHeartbeat for the pane
// with the given task ID. No-op if the pane is not found.
func UpdateWorkerHeartbeat(runtimeDir, taskID string) error {
	panes, err := readPanes(runtimeDir)
	if err != nil {
		return err
	}
	changed := false
	for i := range panes {
		if panes[i].TaskID == taskID {
			panes[i].LastHeartbeat = time.Now().UTC()
			changed = true
			break
		}
	}
	if !changed {
		return nil
	}
	return writePanes(runtimeDir, panes)
}

// ErrNoHeartbeat is returned when no heartbeat file exists.
var ErrNoHeartbeat = errors.New("no heartbeat file")

// ErrNoQueue is returned when no queue.json exists.
var ErrNoQueue = errors.New("no queue session")

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
