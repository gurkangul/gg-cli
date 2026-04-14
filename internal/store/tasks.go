package store

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/qdrant/go-client/qdrant"
)

// taskIDNamespace seeds the deterministic UUID derived from a task_id.
// This guarantees concurrent `gg task create` calls with the same logical
// ID would upsert into the same Qdrant point rather than creating duplicates.
var taskIDNamespace = uuid.MustParse("c0c0c0c0-1a5c-4d0d-bab0-000000000001")

var taskIDRegex = regexp.MustCompile(`^TASK-\d{3,}$`)

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

// scrollAll paginates through every point matching the given request template.
// It builds a fresh ScrollPoints internally (avoiding the protobuf copylocks
// hazard of cloning the caller's struct) and uses Limit as the page size.
func (c *Client) scrollAll(ctx context.Context, req *qdrant.ScrollPoints) ([]*qdrant.RetrievedPoint, error) {
	pageSize := uint32(256)
	if req.Limit != nil {
		pageSize = *req.Limit
	}
	var all []*qdrant.RetrievedPoint
	var offset *qdrant.PointId
	for {
		page, next, err := c.qc.ScrollAndOffset(ctx, &qdrant.ScrollPoints{
			CollectionName:   req.CollectionName,
			Filter:           req.Filter,
			Limit:            &pageSize,
			Offset:           offset,
			WithPayload:      req.WithPayload,
			WithVectors:      req.WithVectors,
			OrderBy:          req.OrderBy,
			ReadConsistency:  req.ReadConsistency,
			ShardKeySelector: req.ShardKeySelector,
			Timeout:          req.Timeout,
		})
		if err != nil {
			return nil, err
		}
		all = append(all, page...)
		if next == nil {
			break
		}
		offset = next
	}
	return all, nil
}

func pointUUIDForTaskID(taskID string) string {
	return uuid.NewSHA1(taskIDNamespace, []byte(taskID)).String()
}

// maxTaskIDNumber scans Qdrant for the highest existing task_id suffix.
// Only used to bootstrap the seq file on first allocation.
func (c *Client) maxTaskIDNumber(ctx context.Context) (int, error) {
	points, err := c.scrollAll(ctx, &qdrant.ScrollPoints{
		CollectionName: CollTasks,
		Limit:          qdrant.PtrOf(uint32(1000)),
		WithPayload:    qdrant.NewWithPayloadInclude("task_id"),
	})
	if err != nil {
		return 0, fmt.Errorf("scan tasks collection (did you run `gg init`?): %w", err)
	}
	maxNum := 0
	for _, p := range points {
		id := p.GetPayload()["task_id"].GetStringValue()
		if n, err := ParseTaskID(id); err == nil && n > maxNum {
			maxNum = n
		}
	}
	return maxNum, nil
}

func (c *Client) CreateTask(ctx context.Context, t Task, vector []float32) (string, error) {
	id, err := c.allocTaskID(ctx)
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

	payload, err := qdrant.TryValueMap(map[string]any{
		"task_id":      t.ID,
		"title":        t.Title,
		"detail":       t.Detail,
		"status":       t.Status,
		"priority":     t.Priority,
		"depends_on":   toAnySlice(t.DependsOn),
		"tags":         toAnySlice(t.Tags),
		"block_reason": t.BlockReason,
		"done_summary": t.DoneSummary,
		"created_at":   t.CreatedAt,
	})
	if err != nil {
		return "", fmt.Errorf("build payload: %w", err)
	}

	wait := true
	// Deterministic point UUID — concurrent create with same task_id collapses
	// to one row instead of creating duplicates.
	_, err = c.qc.Upsert(ctx, &qdrant.UpsertPoints{
		CollectionName: CollTasks,
		Wait:           &wait,
		Points: []*qdrant.PointStruct{
			{
				Id:      qdrant.NewID(pointUUIDForTaskID(t.ID)),
				Vectors: qdrant.NewVectors(vector...),
				Payload: payload,
			},
		},
	})
	if err != nil {
		return "", err
	}
	return t.ID, nil
}

func (c *Client) ListTasks(ctx context.Context, statusFilter string) ([]Task, error) {
	req := &qdrant.ScrollPoints{
		CollectionName: CollTasks,
		WithPayload:    qdrant.NewWithPayloadEnable(true),
	}
	if statusFilter != "" {
		req.Filter = &qdrant.Filter{
			Must: []*qdrant.Condition{
				qdrant.NewMatchKeyword("status", statusFilter),
			},
		}
	}
	points, err := c.scrollAll(ctx, req)
	if err != nil {
		return nil, err
	}
	tasks := make([]Task, 0, len(points))
	for _, p := range points {
		tasks = append(tasks, taskFromRetrieved(p))
	}
	// Sort by numeric task ID suffix so TASK-1000 follows TASK-999.
	sort.Slice(tasks, func(i, j int) bool {
		ni, _ := ParseTaskID(tasks[i].ID)
		nj, _ := ParseTaskID(tasks[j].ID)
		return ni < nj
	})
	return tasks, nil
}

func (c *Client) GetTask(ctx context.Context, taskID string) (*Task, error) {
	points, err := c.qc.Get(ctx, &qdrant.GetPoints{
		CollectionName: CollTasks,
		Ids:            []*qdrant.PointId{qdrant.NewID(pointUUIDForTaskID(taskID))},
		WithPayload:    qdrant.NewWithPayloadEnable(true),
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
	pointID := qdrant.NewID(pointUUIDForTaskID(taskID))
	// Verify the task exists before updating.
	existing, err := c.qc.Get(ctx, &qdrant.GetPoints{
		CollectionName: CollTasks,
		Ids:            []*qdrant.PointId{pointID},
		WithPayload:    qdrant.NewWithPayloadInclude("task_id"),
	})
	if err != nil {
		return err
	}
	if len(existing) == 0 {
		return fmt.Errorf("task %s not found", taskID)
	}

	statusVal, _ := qdrant.NewValue(status)
	emptyVal, _ := qdrant.NewValue("")
	payload := map[string]*qdrant.Value{
		"status": statusVal,
	}
	switch status {
	case "done":
		payload["done_summary"], _ = qdrant.NewValue(extra)
		payload["block_reason"] = emptyVal
	case "blocked":
		payload["block_reason"], _ = qdrant.NewValue(extra)
		payload["done_summary"] = emptyVal
	case "pending", "in_progress":
		payload["block_reason"] = emptyVal
		payload["done_summary"] = emptyVal
	}

	wait := true
	_, err = c.qc.SetPayload(ctx, &qdrant.SetPayloadPoints{
		CollectionName: CollTasks,
		Wait:           &wait,
		Payload:        payload,
		PointsSelector: qdrant.NewPointsSelector(pointID),
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
	if !taskIDRegex.MatchString(id) {
		return 0, fmt.Errorf("invalid task ID %q (expected TASK-NNN)", id)
	}
	return strconv.Atoi(id[5:])
}
