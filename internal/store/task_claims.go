package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/qdrant/go-client/qdrant"
)

var (
	ErrTaskClaimed            = errors.New("task already claimed")
	ErrTaskTransitionConflict = errors.New("task transition conflict")
)

func taskStringValue(s string) *qdrant.Value {
	v, _ := qdrant.NewValue(s)
	return v
}

func taskMutationFilter(taskID string, statuses ...string) *qdrant.Filter {
	conditions := []*qdrant.Condition{qdrant.NewMatchKeyword("task_id", taskID)}
	switch len(statuses) {
	case 0:
	case 1:
		conditions = append(conditions, qdrant.NewMatchKeyword("status", statuses[0]))
	default:
		conditions = append(conditions, qdrant.NewMatchKeywords("status", statuses...))
	}
	return &qdrant.Filter{Must: conditions}
}

func taskCurrentMutationFilter(current *Task, statuses ...string) *qdrant.Filter {
	f := taskMutationFilter(current.ID, statuses...)
	if current.Version > 0 {
		f.Must = append(f.Must, qdrant.NewMatchInt("task_version", current.Version))
	}
	return f
}

func taskOwnedMutationFilter(taskID, status, owner string) *qdrant.Filter {
	f := taskMutationFilter(taskID, status)
	f.Must = append(f.Must, qdrant.NewMatchKeyword("owner", owner))
	return f
}

func taskCurrentOwnedMutationFilter(current *Task, status, owner string) *qdrant.Filter {
	f := taskCurrentMutationFilter(current, status)
	f.Must = append(f.Must, qdrant.NewMatchKeyword("owner", owner))
	return f
}

func taskVersionedPayload(current *Task, payload map[string]*qdrant.Value, now time.Time) map[string]*qdrant.Value {
	if payload == nil {
		payload = map[string]*qdrant.Value{}
	}
	next := current.Version + 1
	if next <= 0 {
		next = 1
	}
	payload["task_version"] = taskIntValue(next)
	payload["updated_at"] = taskStringValue(now.UTC().Format(time.RFC3339))
	return payload
}

func taskIntValue(n int64) *qdrant.Value {
	v, _ := qdrant.NewValue(n)
	return v
}

func (c *Client) setTaskPayloadByFilter(ctx context.Context, filter *qdrant.Filter, payload map[string]*qdrant.Value) error {
	wait := true
	_, err := c.qc.SetPayload(ctx, &qdrant.SetPayloadPoints{
		CollectionName:   c.collTasks(),
		Wait:             &wait,
		Payload:          payload,
		PointsSelector:   qdrant.NewPointsSelectorFilter(filter),
		ShardKeySelector: nil,
	})
	return err
}

func taskLeaseActive(t *Task, now time.Time) bool {
	if t == nil || strings.TrimSpace(t.LeaseUntil) == "" {
		return false
	}
	leaseUntil, err := time.Parse(time.RFC3339, t.LeaseUntil)
	if err != nil {
		return false
	}
	return leaseUntil.After(now)
}

func requireTaskOwner(owner string) (string, error) {
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return "", fmt.Errorf("owner is required")
	}
	return owner, nil
}

func requirePositiveLease(lease time.Duration) error {
	if lease <= 0 {
		return fmt.Errorf("lease must be greater than zero")
	}
	return nil
}

func (c *Client) StartTask(ctx context.Context, taskID, owner string, lease time.Duration) (*Task, error) {
	owner, err := requireTaskOwner(owner)
	if err != nil {
		return nil, err
	}
	if err := requirePositiveLease(lease); err != nil {
		return nil, err
	}
	current, err := c.GetTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	switch current.Status {
	case "pending":
	case "in_progress":
	default:
		return nil, fmt.Errorf("%w: cannot start %s from status %q", ErrTaskTransitionConflict, taskID, current.Status)
	}
	if current.Owner != "" && current.Owner != owner && taskLeaseActive(current, now) {
		return nil, fmt.Errorf("%w: %s is owned by %s until %s", ErrTaskClaimed, taskID, current.Owner, current.LeaseUntil)
	}

	leaseUntil := now.Add(lease).Format(time.RFC3339)
	payload := map[string]*qdrant.Value{
		"status":       taskStringValue("in_progress"),
		"owner":        taskStringValue(owner),
		"claimed_at":   taskStringValue(now.Format(time.RFC3339)),
		"lease_until":  taskStringValue(leaseUntil),
		"block_reason": taskStringValue(""),
		"done_summary": taskStringValue(""),
	}
	payload = taskVersionedPayload(current, payload, now)
	filter := taskCurrentMutationFilter(current, current.Status)
	if current.Owner != "" {
		filter = taskCurrentOwnedMutationFilter(current, current.Status, current.Owner)
	}
	if err := c.setTaskPayloadByFilter(ctx, filter, payload); err != nil {
		return nil, err
	}
	updated, err := c.GetTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if updated.Status != "in_progress" || updated.Owner != owner || updated.LeaseUntil != leaseUntil {
		if updated.Owner != "" && updated.Owner != owner {
			return nil, fmt.Errorf("%w: %s is owned by %s until %s", ErrTaskClaimed, taskID, updated.Owner, updated.LeaseUntil)
		}
		return nil, fmt.Errorf("%w: start %s expected owner=%s status=in_progress, got owner=%s status=%s", ErrTaskTransitionConflict, taskID, owner, updated.Owner, updated.Status)
	}
	if err := c.AppendTaskEvent(TaskEvent{
		TaskID:     taskID,
		Action:     "started",
		FromStatus: current.Status,
		ToStatus:   updated.Status,
		Owner:      owner,
		LeaseUntil: leaseUntil,
		Actor:      owner,
	}); err != nil {
		return nil, err
	}
	return updated, nil
}

func (c *Client) RenewTask(ctx context.Context, taskID, owner string, lease time.Duration) (*Task, error) {
	owner, err := requireTaskOwner(owner)
	if err != nil {
		return nil, err
	}
	if err := requirePositiveLease(lease); err != nil {
		return nil, err
	}
	current, err := c.GetTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if current.Status != "in_progress" {
		return nil, fmt.Errorf("%w: cannot renew %s from status %q", ErrTaskTransitionConflict, taskID, current.Status)
	}
	if current.Owner != owner {
		return nil, fmt.Errorf("%w: %s is owned by %s", ErrTaskClaimed, taskID, current.Owner)
	}
	now := time.Now().UTC()
	leaseUntil := now.Add(lease).Format(time.RFC3339)
	payload := taskVersionedPayload(current, map[string]*qdrant.Value{
		"lease_until": taskStringValue(leaseUntil),
	}, now)
	if err := c.setTaskPayloadByFilter(ctx, taskCurrentOwnedMutationFilter(current, "in_progress", owner), payload); err != nil {
		return nil, err
	}
	updated, err := c.GetTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if updated.Status != "in_progress" || updated.Owner != owner || updated.LeaseUntil != leaseUntil {
		return nil, fmt.Errorf("%w: renew %s expected owner=%s, got owner=%s status=%s", ErrTaskTransitionConflict, taskID, owner, updated.Owner, updated.Status)
	}
	if err := c.AppendTaskEvent(TaskEvent{
		TaskID:     taskID,
		Action:     "renewed",
		FromStatus: current.Status,
		ToStatus:   updated.Status,
		Owner:      owner,
		LeaseUntil: leaseUntil,
		Actor:      owner,
	}); err != nil {
		return nil, err
	}
	return updated, nil
}

func (c *Client) ReleaseTask(ctx context.Context, taskID, owner string) (*Task, error) {
	owner, err := requireTaskOwner(owner)
	if err != nil {
		return nil, err
	}
	current, err := c.GetTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if current.Status != "in_progress" {
		return nil, fmt.Errorf("%w: cannot release %s from status %q", ErrTaskTransitionConflict, taskID, current.Status)
	}
	if current.Owner != owner {
		return nil, fmt.Errorf("%w: %s is owned by %s", ErrTaskClaimed, taskID, current.Owner)
	}
	payload := taskVersionedPayload(current, map[string]*qdrant.Value{
		"status":      taskStringValue("pending"),
		"owner":       taskStringValue(""),
		"claimed_at":  taskStringValue(""),
		"lease_until": taskStringValue(""),
	}, time.Now().UTC())
	if err := c.setTaskPayloadByFilter(ctx, taskCurrentOwnedMutationFilter(current, "in_progress", owner), payload); err != nil {
		return nil, err
	}
	updated, err := c.GetTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if updated.Status != "pending" || updated.Owner != "" {
		return nil, fmt.Errorf("%w: release %s expected unowned pending task, got owner=%s status=%s", ErrTaskTransitionConflict, taskID, updated.Owner, updated.Status)
	}
	if err := c.AppendTaskEvent(TaskEvent{
		TaskID:     taskID,
		Action:     "released",
		FromStatus: current.Status,
		ToStatus:   updated.Status,
		Owner:      owner,
		Actor:      owner,
	}); err != nil {
		return nil, err
	}
	return updated, nil
}
