package store

import (
	"context"
	"fmt"
	"time"
)

// ErrAlreadyInState is returned by UpdateTaskStatus when the task is already
// in the requested status.
var ErrAlreadyInState = fmt.Errorf("task already in target state")

func (c *Client) UpdateTaskStatus(ctx context.Context, taskID, status, extra string) error {
	return c.UpdateTaskStatusCAS(ctx, taskID, "", status, extra)
}

func (c *Client) UpdateTaskStatusCAS(ctx context.Context, taskID, expectedStatus, status, extra string) error {
	current, err := c.GetTask(ctx, taskID)
	if err != nil {
		return err
	}
	if expectedStatus != "" && current.Status != expectedStatus {
		return fmt.Errorf("%w: task %s status is %q, expected %q", ErrTaskTransitionConflict, taskID, current.Status, expectedStatus)
	}
	if current.Status == status {
		return fmt.Errorf("%w: task %s already %s — refusing to overwrite (use --force to clobber)", ErrAlreadyInState, taskID, status)
	}

	payload := map[string]*Value{"status": taskStringValue(status)}
	switch status {
	case "done":
		payload["done_summary"] = taskStringValue(extra)
		payload["block_reason"] = taskStringValue("")
	case "blocked":
		payload["block_reason"] = taskStringValue(extra)
		payload["done_summary"] = taskStringValue("")
	case "pending", "in_progress":
		payload["block_reason"] = taskStringValue("")
		payload["done_summary"] = taskStringValue("")
	}
	payload = taskVersionedPayload(current, payload, time.Now().UTC())

	if err := c.setTaskPayloadByFilter(ctx, taskCurrentMutationFilter(current, current.Status), payload); err != nil {
		return err
	}
	updated, err := c.GetTask(ctx, taskID)
	if err != nil {
		return err
	}
	if updated.Status != status {
		return fmt.Errorf("%w: task %s expected status %q, got %q", ErrTaskTransitionConflict, taskID, status, updated.Status)
	}
	switch status {
	case "done":
		if updated.DoneSummary != extra {
			return fmt.Errorf("%w: task %s done summary changed concurrently", ErrTaskTransitionConflict, taskID)
		}
	case "blocked":
		if updated.BlockReason != extra {
			return fmt.Errorf("%w: task %s block reason changed concurrently", ErrTaskTransitionConflict, taskID)
		}
	}
	return c.AppendTaskEvent(TaskEvent{
		TaskID:     taskID,
		Action:     status,
		FromStatus: current.Status,
		ToStatus:   updated.Status,
		Owner:      updated.Owner,
		Detail:     extra,
	})
}
