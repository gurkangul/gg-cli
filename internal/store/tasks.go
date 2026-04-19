package store

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
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
	ID           string
	Title        string
	Detail       string
	Status       string
	Priority     string
	DependsOn    []string
	Blocks       []string // task IDs this task is blocking
	Deadline     string   // RFC3339 date (YYYY-MM-DD)
	Tags         []string
	BlockReason  string
	DoneSummary  string
	Author       string // agent role or user that created this task
	Requester    string // user | agent | system — who initiated this task
	CreatedAt    string
	ReviewStatus string // none|pending|approved|rejected — orthogonal to Status lifecycle
	ReviewedBy   string // reviewer role or agent name
	ReviewedAt   string // RFC3339 timestamp
	ReviewNotes  string // optional reviewer notes
	// ready_for_live metadata — set when status == "ready_for_live". Used by the
	// verifier-separation gate to ensure a different actor calls `task done`.
	ReadyForLiveBy   string // role that performed the ready-for-live transition
	ReadyForLiveAt   string // RFC3339 timestamp of the transition
	ReadyForLivePlan string // short verify plan written by the implementer
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
		CollectionName: c.collTasks(),
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
		"blocks":       toAnySlice(t.Blocks),
		"deadline":     t.Deadline,
		"tags":         toAnySlice(t.Tags),
		"block_reason": t.BlockReason,
		"done_summary": t.DoneSummary,
		"author":       t.Author,
		"requester":    t.Requester,
		"created_at":   t.CreatedAt,
	})
	if err != nil {
		return "", fmt.Errorf("build payload: %w", err)
	}

	wait := true
	// Deterministic point UUID — concurrent create with same task_id collapses
	// to one row instead of creating duplicates.
	err = c.qdrantUpsert(ctx, &qdrant.UpsertPoints{
		CollectionName: c.collTasks(),
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
	return c.listTasksFiltered(ctx, statusFilter, false)
}

// ListTasksNeedsReview returns tasks that are done but have not been reviewed
// (review_status == "none" or "pending"). This is the --needs-review filter.
func (c *Client) ListTasksNeedsReview(ctx context.Context) ([]Task, error) {
	return c.listTasksFiltered(ctx, "done", true)
}

func (c *Client) listTasksFiltered(ctx context.Context, statusFilter string, needsReview bool) ([]Task, error) {
	req := &qdrant.ScrollPoints{
		CollectionName: c.collTasks(),
		WithPayload:    qdrant.NewWithPayloadEnable(true),
	}
	var conditions []*qdrant.Condition
	if statusFilter != "" {
		conditions = append(conditions, qdrant.NewMatchKeyword("status", statusFilter))
	}
	if needsReview {
		conditions = append(conditions, qdrant.NewMatchKeywords("review_status", "none", "pending", ""))
	}
	if len(conditions) > 0 {
		req.Filter = &qdrant.Filter{Must: conditions}
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
		CollectionName: c.collTasks(),
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

// ErrAlreadyInState is returned by UpdateTaskStatus when the task is already
// in the requested status. Used to detect lost-update races between agents
// (e.g. two agents both calling `gg task done TASK-X` simultaneously — the
// second one sees the same target state and bails instead of clobbering the
// first agent's summary).
var ErrAlreadyInState = fmt.Errorf("task already in target state")

func (c *Client) UpdateTaskStatus(ctx context.Context, taskID, status, extra string) error {
	pointID := qdrant.NewID(pointUUIDForTaskID(taskID))
	// Verify the task exists AND read current status to guard against the
	// concurrent-update race (no Qdrant CAS — this is best-effort).
	existing, err := c.qc.Get(ctx, &qdrant.GetPoints{
		CollectionName: c.collTasks(),
		Ids:            []*qdrant.PointId{pointID},
		WithPayload:    qdrant.NewWithPayloadInclude("task_id", "status"),
	})
	if err != nil {
		return err
	}
	if len(existing) == 0 {
		return fmt.Errorf("task %s not found", taskID)
	}
	currentStatus := existing[0].GetPayload()["status"].GetStringValue()
	if currentStatus == status {
		return fmt.Errorf("%w: task %s already %s — refusing to overwrite (use --force to clobber)", ErrAlreadyInState, taskID, status)
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
		CollectionName: c.collTasks(),
		Wait:           &wait,
		Payload:        payload,
		PointsSelector: qdrant.NewPointsSelector(pointID),
	})
	return err
}

// ValidReviewStatuses lists the allowed values for review_status.
var ValidReviewStatuses = map[string]bool{
	"none": true, "pending": true, "approved": true, "rejected": true,
}

// SetReadyForLive transitions a task to status "ready_for_live" and records
// the actor + timestamp + verify-plan that accompanied the transition. The
// actor is read by the verifier-separation gate in `gg task done` to enforce
// same-actor-cannot-verify.
//
// Refuses when the task is already in "ready_for_live" (returns
// ErrAlreadyInState) or in terminal status "done" (no backwards transition —
// reopen via `gg task reopen` or `gg bug reopen` instead).
//
// readyBy should be a role/agent name — empty is rejected so the gate has
// something non-empty to compare against on the done side.
func (c *Client) SetReadyForLive(ctx context.Context, taskID, readyBy, plan string) error {
	if strings.TrimSpace(readyBy) == "" {
		return fmt.Errorf("ready_for_live_by is required (use --from or set GG_ROLE)")
	}
	pointID := qdrant.NewID(pointUUIDForTaskID(taskID))
	existing, err := c.qc.Get(ctx, &qdrant.GetPoints{
		CollectionName: c.collTasks(),
		Ids:            []*qdrant.PointId{pointID},
		WithPayload:    qdrant.NewWithPayloadInclude("task_id", "status"),
	})
	if err != nil {
		return err
	}
	if len(existing) == 0 {
		return fmt.Errorf("task %s not found", taskID)
	}
	currentStatus := existing[0].GetPayload()["status"].GetStringValue()
	if currentStatus == "ready_for_live" {
		return fmt.Errorf("%w: task %s already ready_for_live", ErrAlreadyInState, taskID)
	}
	if currentStatus == "done" {
		return fmt.Errorf("cannot transition done task %s back to ready_for_live (reopen first)", taskID)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	statusVal, _ := qdrant.NewValue("ready_for_live")
	byVal, _ := qdrant.NewValue(readyBy)
	atVal, _ := qdrant.NewValue(now)
	planVal, _ := qdrant.NewValue(plan)
	emptyVal, _ := qdrant.NewValue("")
	wait := true
	_, err = c.qc.SetPayload(ctx, &qdrant.SetPayloadPoints{
		CollectionName: c.collTasks(),
		Wait:           &wait,
		Payload: map[string]*qdrant.Value{
			"status":              statusVal,
			"ready_for_live_by":   byVal,
			"ready_for_live_at":   atVal,
			"ready_for_live_plan": planVal,
			"block_reason":        emptyVal,
			"done_summary":        emptyVal,
		},
		PointsSelector: qdrant.NewPointsSelector(pointID),
	})
	return err
}

// UpdateReviewStatus updates the review_status, reviewed_by, reviewed_at, and
// review_notes fields of a task without touching its lifecycle status. The review
// state is orthogonal to the work lifecycle (pending → in_progress → done).
func (c *Client) UpdateReviewStatus(ctx context.Context, taskID, reviewStatus, reviewedBy, reviewNotes string) error {
	if !ValidReviewStatuses[reviewStatus] {
		return fmt.Errorf("review_status must be one of: none, pending, approved, rejected")
	}
	pointID := qdrant.NewID(pointUUIDForTaskID(taskID))

	// Verify the task exists first.
	existing, err := c.qc.Get(ctx, &qdrant.GetPoints{
		CollectionName: c.collTasks(),
		Ids:            []*qdrant.PointId{pointID},
		WithPayload:    qdrant.NewWithPayloadInclude("task_id"),
	})
	if err != nil {
		return err
	}
	if len(existing) == 0 {
		return fmt.Errorf("task %s not found", taskID)
	}

	rsVal, _ := qdrant.NewValue(reviewStatus)
	rbVal, _ := qdrant.NewValue(reviewedBy)
	raVal, _ := qdrant.NewValue(time.Now().UTC().Format(time.RFC3339))
	rnVal, _ := qdrant.NewValue(reviewNotes)

	wait := true
	_, err = c.qc.SetPayload(ctx, &qdrant.SetPayloadPoints{
		CollectionName: c.collTasks(),
		Wait:           &wait,
		Payload: map[string]*qdrant.Value{
			"review_status": rsVal,
			"reviewed_by":   rbVal,
			"reviewed_at":   raVal,
			"review_notes":  rnVal,
		},
		PointsSelector: qdrant.NewPointsSelector(pointID),
	})
	return err
}

// ActiveTasksFilter returns the Qdrant filter that restricts results to tasks
// in an active state (pending or in_progress). Exported as a helper so tests
// can verify it directly.
func ActiveTasksFilter() *qdrant.Filter {
	return &qdrant.Filter{
		Must: []*qdrant.Condition{
			qdrant.NewMatchKeywords("status", "pending", "in_progress"),
		},
	}
}

// SearchTasks performs a semantic search across the tasks collection.
// When includeAll is false (the default for agent consumption), only pending
// and in_progress tasks are returned — done and blocked tasks are suppressed so
// agents don't surface completed or stale work as relevant context.
func (c *Client) SearchTasks(ctx context.Context, vector []float32, limit uint64, includeAll bool) ([]Task, error) {
	req := &qdrant.QueryPoints{
		CollectionName: c.collTasks(),
		Query:          qdrant.NewQuery(vector...),
		Limit:          qdrant.PtrOf(limit),
		WithPayload:    qdrant.NewWithPayloadEnable(true),
	}
	if !includeAll {
		req.Filter = ActiveTasksFilter()
	}
	results, err := c.qdrantQuery(ctx, req)
	if err != nil {
		return nil, err
	}
	tasks := make([]Task, 0, len(results))
	for _, r := range results {
		tasks = append(tasks, taskFromPayload(r.GetPayload()))
	}
	return tasks, nil
}

func taskFromPayload(pay map[string]*qdrant.Value) Task {
	rs := pay["review_status"].GetStringValue()
	if rs == "" {
		rs = "none"
	}
	return Task{
		ID:           pay["task_id"].GetStringValue(),
		Title:        pay["title"].GetStringValue(),
		Detail:       pay["detail"].GetStringValue(),
		Status:       pay["status"].GetStringValue(),
		Priority:     pay["priority"].GetStringValue(),
		DependsOn:    extractStringList(pay["depends_on"]),
		Blocks:       extractStringList(pay["blocks"]),
		Deadline:     pay["deadline"].GetStringValue(),
		Tags:         extractStringList(pay["tags"]),
		BlockReason:  pay["block_reason"].GetStringValue(),
		DoneSummary:  pay["done_summary"].GetStringValue(),
		Author:       pay["author"].GetStringValue(),
		Requester:    pay["requester"].GetStringValue(),
		CreatedAt:    pay["created_at"].GetStringValue(),
		ReviewStatus:     rs,
		ReviewedBy:       pay["reviewed_by"].GetStringValue(),
		ReviewedAt:       pay["reviewed_at"].GetStringValue(),
		ReviewNotes:      pay["review_notes"].GetStringValue(),
		ReadyForLiveBy:   pay["ready_for_live_by"].GetStringValue(),
		ReadyForLiveAt:   pay["ready_for_live_at"].GetStringValue(),
		ReadyForLivePlan: pay["ready_for_live_plan"].GetStringValue(),
	}
}

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
	return c.qc.Count(ctx, req)
}

func taskFromRetrieved(p *qdrant.RetrievedPoint) Task {
	return taskFromPayload(p.GetPayload())
}

// ParseTaskID extracts the numeric suffix from a task ID like "TASK-001".
func ParseTaskID(id string) (int, error) {
	if !taskIDRegex.MatchString(id) {
		return 0, fmt.Errorf("invalid task ID %q (expected TASK-NNN)", id)
	}
	return strconv.Atoi(id[5:])
}
