package spawn

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// WorkerState represents the current lifecycle state of a worker pane.
type WorkerState string

const (
	// WorkerStateWorking means the worker is actively implementing its task.
	WorkerStateWorking WorkerState = "working"
	// WorkerStateIdle means the worker pane is open but no activity has been detected.
	WorkerStateIdle WorkerState = "idle"
	// WorkerStateWaiting means the worker is blocked and needs master attention.
	WorkerStateWaiting WorkerState = "waiting-on-master"
	// WorkerStateReady means the worker committed and wrote an advance sentinel.
	WorkerStateReady WorkerState = "ready"
)

// WorkerPane records a live worker pane. Stored as elements of panes.json.
type WorkerPane struct {
	SurfaceID           string      `json:"surface_id"`
	TaskID              string      `json:"task_id"`
	Agent               string      `json:"agent"`
	SpawnedAt           time.Time   `json:"spawned_at"`
	State               WorkerState `json:"state,omitempty"`
	LastHeartbeat       time.Time   `json:"last_heartbeat,omitempty"`
	TouchedFiles        []string    `json:"touched_files,omitempty"`
	LastNudgeAt         time.Time   `json:"last_nudge_at,omitempty"`
	LastNudgeText       string      `json:"last_nudge_text,omitempty"`
	LastNudgeScreenHash string      `json:"last_nudge_screen_hash,omitempty"`
	StalledNotifiedAt   time.Time   `json:"stalled_notified_at,omitempty"`
}

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

// FindPaneForTask returns the WorkerPane whose TaskID matches taskID.
func FindPaneForTask(runtimeDir, taskID string) (*WorkerPane, error) {
	panes, err := readPanes(runtimeDir)
	if err != nil {
		return nil, err
	}
	for i := range panes {
		if panes[i].TaskID == taskID {
			p := panes[i]
			return &p, nil
		}
	}
	return nil, nil
}

// RemovePaneLocked removes a worker pane under an advisory flock.
func RemovePaneLocked(runtimeDir, taskID string) error {
	lockPath := filepath.Join(Dir(runtimeDir), PanesFile+".lock")
	return withPanesFlock(lockPath, func() error {
		return RemovePane(runtimeDir, taskID)
	})
}

// ListPanes returns all registered worker panes from panes.json.
func ListPanes(runtimeDir string) ([]WorkerPane, error) {
	return readPanes(runtimeDir)
}

// UpdateWorkerState sets the State field on the pane with the given task ID.
func UpdateWorkerState(runtimeDir, taskID string, state WorkerState) error {
	return updatePane(runtimeDir, taskID, func(p *WorkerPane) {
		p.State = state
	})
}

// UpdateWorkerHeartbeat records the current time as LastHeartbeat.
func UpdateWorkerHeartbeat(runtimeDir, taskID string) error {
	return updatePane(runtimeDir, taskID, func(p *WorkerPane) {
		p.LastHeartbeat = time.Now().UTC()
	})
}

// ScreenHash returns a stable hash for terminal screen content snapshots.
func ScreenHash(content []byte) string {
	sum := sha256.Sum256(content)
	return fmt.Sprintf("%x", sum)
}

// UpdateWorkerNudge records that the master sent a prompt to a surface.
func UpdateWorkerNudge(runtimeDir, surfaceID, text, screenHash string) error {
	return updatePaneBySurface(runtimeDir, surfaceID, func(p *WorkerPane) {
		p.LastNudgeAt = time.Now().UTC()
		p.LastNudgeText = text
		p.LastNudgeScreenHash = screenHash
		p.StalledNotifiedAt = time.Time{}
	})
}

// MarkWorkerStalled records a single stall notification for the current nudge cycle.
func MarkWorkerStalled(runtimeDir, taskID string) (bool, error) {
	fresh := false
	err := updatePane(runtimeDir, taskID, func(p *WorkerPane) {
		if p.LastNudgeAt.IsZero() || p.StalledNotifiedAt.After(p.LastNudgeAt) {
			return
		}
		p.State = WorkerStateWaiting
		p.StalledNotifiedAt = time.Now().UTC()
		fresh = true
	})
	return fresh, err
}

func updatePane(runtimeDir, taskID string, fn func(*WorkerPane)) error {
	return updatePaneMatching(runtimeDir, func(p WorkerPane) bool { return p.TaskID == taskID }, fn)
}

func updatePaneBySurface(runtimeDir, surfaceID string, fn func(*WorkerPane)) error {
	return updatePaneMatching(runtimeDir, func(p WorkerPane) bool { return p.SurfaceID == surfaceID }, fn)
}

func updatePaneMatching(runtimeDir string, match func(WorkerPane) bool, fn func(*WorkerPane)) error {
	panes, err := readPanes(runtimeDir)
	if err != nil {
		return err
	}
	for i := range panes {
		if match(panes[i]) {
			fn(&panes[i])
			return writePanes(runtimeDir, panes)
		}
	}
	return nil
}
