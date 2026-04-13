package store

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/qdrant/go-client/qdrant"
)

type Task struct {
	ID          string
	Title       string
	Detail      string
	Status      string
	Priority    string
	DependsOn   []string
	Tags        []string
	BlockReason string
	DoneSummary string
	CreatedAt   string
}

func (c *Client) nextTaskID(ctx context.Context) (string, error) {
	count, err := c.qc.Count(ctx, &qdrant.CountPoints{
		CollectionName: CollTasks,
	})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("TASK-%03d", count+1), nil
}

func (c *Client) CreateTask(ctx context.Context, t Task, vector []float32) (string, error) {
	id, err := c.nextTaskID(ctx)
	if err != nil {
		return "", err
	}
	t.ID = id
	if t.Status == "" {
		t.Status = "pending"
	}
	if t.Priority == "" {
		t.Priority = "medium"
	}
	if t.CreatedAt == "" {
		t.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}

	payload := map[string]any{
		"task_id":      t.ID,
		"title":        t.Title,
		"detail":       t.Detail,
		"status":       t.Status,
		"priority":     t.Priority,
		"depends_on":   t.DependsOn,
		"tags":         t.Tags,
		"block_reason": t.BlockReason,
		"done_summary": t.DoneSummary,
		"created_at":   t.CreatedAt,
	}

	wait := true
	pointID := uuid.New().String()
	_, err = c.qc.Upsert(ctx, &qdrant.UpsertPoints{
		CollectionName: CollTasks,
		Wait:           &wait,
		Points: []*qdrant.PointStruct{
			{
				Id:      qdrant.NewID(pointID),
				Vectors: qdrant.NewVectors(vector...),
				Payload: qdrant.NewValueMap(payload),
			},
		},
	})
	if err != nil {
		return "", err
	}
	return t.ID, nil
}

func (c *Client) ListTasks(ctx context.Context, statusFilter string) ([]Task, error) {
	scroll := &qdrant.ScrollPoints{
		CollectionName: CollTasks,
		Limit:          qdrant.PtrOf(uint32(100)),
		WithPayload:    qdrant.NewWithPayloadEnable(true),
	}
	if statusFilter != "" {
		scroll.Filter = &qdrant.Filter{
			Must: []*qdrant.Condition{
				qdrant.NewMatchKeyword("status", statusFilter),
			},
		}
	}
	points, err := c.qc.Scroll(ctx, scroll)
	if err != nil {
		return nil, err
	}
	var tasks []Task
	for _, p := range points {
		tasks = append(tasks, taskFromRetrieved(p))
	}
	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].ID < tasks[j].ID
	})
	return tasks, nil
}

func (c *Client) GetTask(ctx context.Context, taskID string) (*Task, error) {
	points, err := c.qc.Scroll(ctx, &qdrant.ScrollPoints{
		CollectionName: CollTasks,
		Limit:          qdrant.PtrOf(uint32(1)),
		WithPayload:    qdrant.NewWithPayloadEnable(true),
		Filter: &qdrant.Filter{
			Must: []*qdrant.Condition{
				qdrant.NewMatchKeyword("task_id", taskID),
			},
		},
	})
	if err != nil {
		return nil, err
	}
	if len(points) == 0 {
		return nil, fmt.Errorf("task %s not found", taskID)
	}
	t := taskFromRetrieved(points[0])
	return &t, nil
}

func (c *Client) UpdateTaskStatus(ctx context.Context, taskID, status, extra string) error {
	points, err := c.qc.Scroll(ctx, &qdrant.ScrollPoints{
		CollectionName: CollTasks,
		Limit:          qdrant.PtrOf(uint32(1)),
		WithPayload:    qdrant.NewWithPayloadEnable(true),
		Filter: &qdrant.Filter{
			Must: []*qdrant.Condition{
				qdrant.NewMatchKeyword("task_id", taskID),
			},
		},
	})
	if err != nil {
		return err
	}
	if len(points) == 0 {
		return fmt.Errorf("task %s not found", taskID)
	}

	statusVal, _ := qdrant.NewValue(status)
	payload := map[string]*qdrant.Value{
		"status": statusVal,
	}
	switch status {
	case "done":
		payload["done_summary"], _ = qdrant.NewValue(extra)
	case "blocked":
		payload["block_reason"], _ = qdrant.NewValue(extra)
	}

	wait := true
	_, err = c.qc.SetPayload(ctx, &qdrant.SetPayloadPoints{
		CollectionName: CollTasks,
		Wait:           &wait,
		Payload:        payload,
		PointsSelector: qdrant.NewPointsSelector(points[0].GetId()),
	})
	return err
}

func (c *Client) CountTasks(ctx context.Context, status string) (uint64, error) {
	req := &qdrant.CountPoints{
		CollectionName: CollTasks,
	}
	if status != "" {
		req.Filter = &qdrant.Filter{
			Must: []*qdrant.Condition{
				qdrant.NewMatchKeyword("status", status),
			},
		}
	}
	return c.qc.Count(ctx, req)
}

func taskFromRetrieved(p *qdrant.RetrievedPoint) Task {
	pay := p.GetPayload()
	return Task{
		ID:          pay["task_id"].GetStringValue(),
		Title:       pay["title"].GetStringValue(),
		Detail:      pay["detail"].GetStringValue(),
		Status:      pay["status"].GetStringValue(),
		Priority:    pay["priority"].GetStringValue(),
		DependsOn:   extractStringList(pay["depends_on"]),
		Tags:        extractStringList(pay["tags"]),
		BlockReason: pay["block_reason"].GetStringValue(),
		DoneSummary: pay["done_summary"].GetStringValue(),
		CreatedAt:   pay["created_at"].GetStringValue(),
	}
}

// ParseTaskID extracts the numeric suffix from a task ID like "TASK-001".
func ParseTaskID(id string) (int, error) {
	if len(id) < 6 || id[:5] != "TASK-" {
		return 0, fmt.Errorf("invalid task ID: %s", id)
	}
	return strconv.Atoi(id[5:])
}
