package store

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/gurkangul/gg-cli/internal/brain"
)

const taskEventsKind = "task-events"

// TaskEvent is the append-only audit trail for task lifecycle mutations. the vector store
// holds the current task projection; this log records successful state changes
// so a future replay path can rebuild that projection from JSONL.
type TaskEvent struct {
	ID         string
	TaskID     string
	Action     string
	FromStatus string
	ToStatus   string
	Owner      string
	LeaseUntil string
	Actor      string
	Detail     string
	CreatedAt  string
}

func (c *Client) AppendTaskEvent(e TaskEvent) error {
	if e.TaskID == "" {
		return fmt.Errorf("task event task_id is required")
	}
	if e.Action == "" {
		return fmt.Errorf("task event action is required")
	}
	if e.ID == "" {
		e.ID = uuid.NewString()
	}
	if e.CreatedAt == "" {
		e.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	payload := map[string]any{
		"task_id":     e.TaskID,
		"action":      e.Action,
		"from_status": e.FromStatus,
		"to_status":   e.ToStatus,
		"owner":       e.Owner,
		"lease_until": e.LeaseUntil,
		"actor":       e.Actor,
		"detail":      e.Detail,
		"created_at":  e.CreatedAt,
	}
	if err := brain.Append(c.dataDir, taskEventsKind, e.ID, e.Actor, payload); err != nil {
		return fmt.Errorf("task event append: %w", err)
	}
	return nil
}
