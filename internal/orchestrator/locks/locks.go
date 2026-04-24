// Package locks provides advisory file-path locking for the gg spawn queue.
// A task can claim a set of file paths; subsequent spawn attempts that overlap
// are blocked with a clear collision error unless --force is passed.
//
// Claim state lives at ~/.gg/projects/<project_id>/spawn/locks.json as a flat
// JSON array — no daemon, no kernel lock, pure advisory.
package locks

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// LocksFile is the name of the advisory lock file under the spawn dir.
const LocksFile = "locks.json"

// Claim records one task's advisory claim over a set of file paths.
type Claim struct {
	// TaskID is the task that holds the claim.
	TaskID string `json:"task_id"`
	// Paths is the set of file paths claimed by this task.
	Paths []string `json:"paths"`
	// ClaimedAt is when the claim was written.
	ClaimedAt time.Time `json:"claimed_at"`
}

// Collision describes a path conflict between a new task and an existing claim.
type Collision struct {
	// Path is the file that is already claimed.
	Path string
	// HeldBy is the task ID that currently holds the claim.
	HeldBy string
}

// Error implements the error interface for a Collision.
func (c Collision) Error() string {
	return fmt.Sprintf("collision: %s already claims %s", c.HeldBy, c.Path)
}

// CollisionList is a slice of Collision values that implements error.
type CollisionList []Collision

func (cl CollisionList) Error() string {
	if len(cl) == 1 {
		return cl[0].Error()
	}
	msg := fmt.Sprintf("%d path collisions:", len(cl))
	for _, c := range cl {
		msg += "\n  • " + c.Error()
	}
	return msg
}

// Store manages advisory claims for a single project's spawn directory.
type Store struct {
	spawnDir string
}

// New returns a Store rooted at spawnDir (the spawn sub-dir of the runtime dir).
func New(spawnDir string) *Store {
	return &Store{spawnDir: spawnDir}
}

func (s *Store) path() string {
	return filepath.Join(s.spawnDir, LocksFile)
}

// ReadAll returns all active claims. Returns nil slice when the file is absent.
func (s *Store) ReadAll() ([]Claim, error) {
	b, err := os.ReadFile(s.path())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read locks.json: %w", err)
	}
	var claims []Claim
	if err := json.Unmarshal(b, &claims); err != nil {
		return nil, fmt.Errorf("parse locks.json: %w", err)
	}
	return claims, nil
}

// writeAll writes the full claims slice atomically (via temp file + rename).
func (s *Store) writeAll(claims []Claim) error {
	if err := os.MkdirAll(s.spawnDir, 0o700); err != nil {
		return fmt.Errorf("create spawn dir: %w", err)
	}
	b, err := json.MarshalIndent(claims, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal locks: %w", err)
	}
	b = append(b, '\n')

	tmp := s.path() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return fmt.Errorf("write locks tmp: %w", err)
	}
	if err := os.Rename(tmp, s.path()); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename locks: %w", err)
	}
	return nil
}

// CheckConflicts returns collisions between paths and currently active claims,
// ignoring any claim held by excludeTaskID (so a task may re-claim its own files).
func (s *Store) CheckConflicts(paths []string, excludeTaskID string) (CollisionList, error) {
	claims, err := s.ReadAll()
	if err != nil {
		return nil, err
	}

	// Build a flat map: path → holder task ID.
	held := make(map[string]string, len(claims)*4)
	for _, c := range claims {
		if c.TaskID == excludeTaskID {
			continue
		}
		for _, p := range c.Paths {
			held[p] = c.TaskID
		}
	}

	var collisions CollisionList
	for _, p := range paths {
		if holder, ok := held[p]; ok {
			collisions = append(collisions, Collision{Path: p, HeldBy: holder})
		}
	}
	return collisions, nil
}

// Acquire records a claim for taskID over paths, replacing any prior claim for
// that task. It checks conflicts first; returns CollisionList on conflict.
// Pass force=true to skip the conflict check (logs the override, writes anyway).
func (s *Store) Acquire(taskID string, paths []string, force bool) error {
	if !force {
		collisions, err := s.CheckConflicts(paths, taskID)
		if err != nil {
			return err
		}
		if len(collisions) > 0 {
			return collisions
		}
	}

	claims, err := s.ReadAll()
	if err != nil {
		return err
	}

	// Remove any existing claim for this task.
	filtered := claims[:0]
	for _, c := range claims {
		if c.TaskID != taskID {
			filtered = append(filtered, c)
		}
	}

	if len(paths) > 0 {
		filtered = append(filtered, Claim{
			TaskID:    taskID,
			Paths:     paths,
			ClaimedAt: time.Now().UTC(),
		})
	}

	return s.writeAll(filtered)
}

// Release removes any claim held by taskID. No-op if the task has no claim.
func (s *Store) Release(taskID string) error {
	claims, err := s.ReadAll()
	if err != nil {
		return err
	}

	filtered := claims[:0]
	for _, c := range claims {
		if c.TaskID != taskID {
			filtered = append(filtered, c)
		}
	}

	if len(filtered) == len(claims) {
		return nil // nothing changed
	}
	return s.writeAll(filtered)
}

// PathsFor returns the paths claimed by taskID, or nil if it has no claim.
func (s *Store) PathsFor(taskID string) ([]string, error) {
	claims, err := s.ReadAll()
	if err != nil {
		return nil, err
	}
	for _, c := range claims {
		if c.TaskID == taskID {
			return c.Paths, nil
		}
	}
	return nil, nil
}
