package store

import (
	"context"
	"fmt"

	"github.com/qdrant/go-client/qdrant"
)

func (c *Client) CountTasks(ctx context.Context, status string) (uint64, error) {
	req := &qdrant.CountPoints{
		CollectionName: c.collTasks(),
	}
	if status != "" {
		req.Filter = &qdrant.Filter{
			Must: []*qdrant.Condition{
				qdrant.NewMatchKeyword("status", status),
			},
		}
	}
	return c.vs.Count(ctx, req)
}

func taskFromRetrieved(p *qdrant.RetrievedPoint) Task {
	return taskFromPayload(p.GetPayload())
}

// CancelTask permanently removes a task from Qdrant. The point is deleted by
// its deterministic UUID so no scan is needed. The caller is responsible for
// also removing the Task node from Memgraph (see cmd/task_cancel.go).
func (c *Client) CancelTask(ctx context.Context, taskID, actor, reason string) error {
	if _, err := ParseTaskID(taskID); err != nil {
		return err
	}
	current, err := c.GetTask(ctx, taskID)
	if err != nil {
		return err
	}
	ptID := qdrant.NewID(pointUUIDForTaskID(taskID))
	wait := true
	_, err = c.vs.Delete(ctx, &qdrant.DeletePoints{
		CollectionName: c.collTasks(),
		Wait:           &wait,
		Points:         qdrant.NewPointsSelector(ptID),
	})
	if err != nil {
		return fmt.Errorf("cancel task %s: %w", taskID, err)
	}
	return c.AppendTaskEvent(TaskEvent{
		TaskID:     taskID,
		Action:     "cancelled",
		FromStatus: current.Status,
		ToStatus:   "cancelled",
		Owner:      current.Owner,
		Actor:      actor,
		Detail:     reason,
	})
}
