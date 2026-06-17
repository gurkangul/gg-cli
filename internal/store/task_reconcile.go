package store

import (
	"context"
	"fmt"
)

// SetTaskProjectionPayload is a narrow repair hook for gg reconcile. It updates
// the current task projection from the append-only task event log without
// allocating IDs or writing a new create record.
func (c *Client) SetTaskProjectionPayload(ctx context.Context, taskID string, fields map[string]any) error {
	if _, err := ParseTaskID(taskID); err != nil {
		return err
	}
	if len(fields) == 0 {
		return nil
	}
	payload, err := TryValueMap(fields)
	if err != nil {
		return fmt.Errorf("build task projection payload: %w", err)
	}
	wait := true
	_, err = c.vs.SetPayload(ctx, &SetPayloadPoints{
		CollectionName: c.collTasks(),
		Wait:           &wait,
		Payload:        payload,
		PointsSelector: NewPointsSelector(NewID(pointUUIDForTaskID(taskID))),
	})
	if err != nil {
		return fmt.Errorf("set task projection %s: %w", taskID, err)
	}
	return nil
}
