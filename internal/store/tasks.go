package store

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gurkangul/gg-cli/internal/brain"
	"github.com/qdrant/go-client/qdrant"
)

// taskIDNamespace seeds the deterministic UUID derived from a task_id.
// This guarantees concurrent `gg task create` calls with the same logical
// ID would upsert into the same Qdrant point rather than creating duplicates.
var taskIDNamespace = uuid.MustParse("c0c0c0c0-1a5c-4d0d-bab0-000000000001")

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
	UpdatedAt    string
	Version      int64
	Owner        string // agent currently holding the task claim
	ClaimedAt    string // RFC3339 timestamp when Owner claimed the task
	LeaseUntil   string // RFC3339 timestamp when the current claim expires
	ReviewStatus string // none|pending|approved|rejected — orthogonal to Status lifecycle
	ReviewedBy   string // reviewer role or agent name
	ReviewedAt   string // RFC3339 timestamp
	ReviewNotes  string // optional reviewer notes
	// ready_for_live metadata — set when status == "ready_for_live". Used by the
	// verifier-separation gate to ensure a different actor calls `task done`.
	ReadyForLiveBy   string  // role that performed the ready-for-live transition
	ReadyForLiveAt   string  // RFC3339 timestamp of the transition
	ReadyForLivePlan string  // short verify plan written by the implementer
	SemanticScore    float32 `json:"semantic_score,omitempty"`
	VectorDegraded   bool    `json:"vector_degraded,omitempty"`
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

// CreateTask writes the task to .gg/brain/tasks.jsonl first (AC-1: durable,
// offline-safe), then attempts a Qdrant upsert (AC-2).
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
	if t.UpdatedAt == "" {
		t.UpdatedAt = t.CreatedAt
	}
	if t.Version <= 0 {
		t.Version = 1
	}

	rawPayload := map[string]any{
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
		"updated_at":   t.UpdatedAt,
		"task_version": t.Version,
		"owner":        t.Owner,
		"claimed_at":   t.ClaimedAt,
		"lease_until":  t.LeaseUntil,
	}

	// AC-1: JSONL write first — survives Qdrant downtime.
	// Use deterministic point UUID as the brain uuid so outbox replay finds the right point.
	brainUUID := pointUUIDForTaskID(t.ID)
	if err := brain.Append(c.dataDir, "tasks", brainUUID, t.Author, rawPayload); err != nil {
		return "", fmt.Errorf("brain jsonl write: %w", err)
	}
	if err := c.AppendTaskEvent(TaskEvent{
		TaskID:   t.ID,
		Action:   "created",
		ToStatus: t.Status,
		Owner:    t.Owner,
		Actor:    t.Author,
		Detail:   t.Title,
	}); err != nil {
		return "", err
	}
	if len(vector) == 0 {
		return t.ID, semanticVectorMissing(OutboxKindTask, brainUUID)
	}

	// AC-2: Qdrant upsert is secondary best-effort.
	qdrantPayload, err := qdrant.TryValueMap(rawPayload)
	if err != nil {
		return "", fmt.Errorf("build payload: %w", err)
	}
	wait := true
	// Deterministic point UUID — concurrent create with same task_id collapses
	// to one row instead of creating duplicates.
	uErr := c.qdrantUpsert(ctx, &qdrant.UpsertPoints{
		CollectionName: c.collTasks(),
		Wait:           &wait,
		Points: []*qdrant.PointStruct{
			{
				Id:      qdrant.NewID(brainUUID),
				Vectors: qdrant.NewVectors(vector...),
				Payload: qdrantPayload,
			},
		},
	})
	if uErr != nil {
		return t.ID, &OutboxQueued{Kind: OutboxKindTask, UUID: brainUUID, Cause: uErr}
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

func readyForLiveActionForStatus(status string) string {
	if status == "ready_for_live" {
		return "ready_for_live_updated"
	}
	return "ready_for_live"
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
// If the task is already in "ready_for_live", this updates the verifier plan
// in place and appends a "ready_for_live_updated" event. Terminal "done"
// tasks are still refused (no backwards transition — reopen via
// `gg task reopen` or `gg bug reopen` instead).
//
// readyBy should be a role/agent name — empty is rejected so the gate has
// something non-empty to compare against on the done side.
func (c *Client) SetReadyForLive(ctx context.Context, taskID, readyBy, plan string) error {
	if strings.TrimSpace(readyBy) == "" {
		return fmt.Errorf("ready_for_live_by is required (use --from or set GG_ROLE)")
	}
	current, err := c.GetTask(ctx, taskID)
	if err != nil {
		return err
	}
	currentStatus := current.Status
	if currentStatus == "done" {
		return fmt.Errorf("cannot transition done task %s back to ready_for_live (reopen first)", taskID)
	}
	action := readyForLiveActionForStatus(currentStatus)

	now := time.Now().UTC().Format(time.RFC3339)
	payload := map[string]*qdrant.Value{
		"status":              taskStringValue("ready_for_live"),
		"ready_for_live_by":   taskStringValue(readyBy),
		"ready_for_live_at":   taskStringValue(now),
		"ready_for_live_plan": taskStringValue(plan),
		"block_reason":        taskStringValue(""),
		"done_summary":        taskStringValue(""),
	}
	payload = taskVersionedPayload(current, payload, time.Now().UTC())
	if err := c.setTaskPayloadByFilter(ctx, taskCurrentMutationFilter(current, currentStatus), payload); err != nil {
		return err
	}
	updated, err := c.GetTask(ctx, taskID)
	if err != nil {
		return err
	}
	if updated.Status != "ready_for_live" {
		return fmt.Errorf("%w: task %s expected ready_for_live, got %q", ErrTaskTransitionConflict, taskID, updated.Status)
	}
	if updated.ReadyForLiveBy != readyBy || updated.ReadyForLivePlan != plan {
		return fmt.Errorf("%w: task %s ready_for_live metadata changed concurrently", ErrTaskTransitionConflict, taskID)
	}
	return c.AppendTaskEvent(TaskEvent{
		TaskID:     taskID,
		Action:     action,
		FromStatus: currentStatus,
		ToStatus:   updated.Status,
		Owner:      updated.Owner,
		Actor:      readyBy,
		Detail:     plan,
	})
}

// UpdateReviewStatus updates the review_status, reviewed_by, reviewed_at, and
// review_notes fields of a task without touching its lifecycle status. The review
// state is orthogonal to the work lifecycle (pending → in_progress → done).
func (c *Client) UpdateReviewStatus(ctx context.Context, taskID, reviewStatus, reviewedBy, reviewNotes string) error {
	if !ValidReviewStatuses[reviewStatus] {
		return fmt.Errorf("review_status must be one of: none, pending, approved, rejected")
	}
	current, err := c.GetTask(ctx, taskID)
	if err != nil {
		return err
	}

	rsVal, _ := qdrant.NewValue(reviewStatus)
	rbVal, _ := qdrant.NewValue(reviewedBy)
	raVal, _ := qdrant.NewValue(time.Now().UTC().Format(time.RFC3339))
	rnVal, _ := qdrant.NewValue(reviewNotes)

	payload := map[string]*qdrant.Value{
		"review_status": rsVal,
		"reviewed_by":   rbVal,
		"reviewed_at":   raVal,
		"review_notes":  rnVal,
	}
	payload = taskVersionedPayload(current, payload, time.Now().UTC())
	if err := c.setTaskPayloadByFilter(ctx, taskCurrentMutationFilter(current, current.Status), payload); err != nil {
		return err
	}
	updated, err := c.GetTask(ctx, taskID)
	if err != nil {
		return err
	}
	if updated.ReviewStatus != reviewStatus || updated.ReviewedBy != reviewedBy || updated.ReviewNotes != reviewNotes {
		return fmt.Errorf("%w: task %s review metadata changed concurrently", ErrTaskTransitionConflict, taskID)
	}
	return c.AppendTaskEvent(TaskEvent{
		TaskID: taskID,
		Action: "reviewed",
		Actor:  reviewedBy,
		Detail: reviewStatus + ": " + reviewNotes,
	})
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
		f := ActiveTasksFilter()
		f.Must = append(f.Must, nonDegradedVectorCondition())
		req.Filter = f
	} else {
		req.Filter = &qdrant.Filter{Must: []*qdrant.Condition{nonDegradedVectorCondition()}}
	}
	results, err := c.qdrantQuery(ctx, req)
	if err != nil {
		return nil, err
	}
	tasks := make([]Task, 0, len(results))
	for _, r := range results {
		t := taskFromPayload(r.GetPayload())
		t.SemanticScore = r.GetScore()
		tasks = append(tasks, t)
	}
	return tasks, nil
}

func taskFromPayload(pay map[string]*qdrant.Value) Task {
	rs := pay["review_status"].GetStringValue()
	if rs == "" {
		rs = "none"
	}
	return Task{
		ID:               pay["task_id"].GetStringValue(),
		Title:            pay["title"].GetStringValue(),
		Detail:           pay["detail"].GetStringValue(),
		Status:           pay["status"].GetStringValue(),
		Priority:         pay["priority"].GetStringValue(),
		DependsOn:        extractStringList(pay["depends_on"]),
		Blocks:           extractStringList(pay["blocks"]),
		Deadline:         pay["deadline"].GetStringValue(),
		Tags:             extractStringList(pay["tags"]),
		BlockReason:      pay["block_reason"].GetStringValue(),
		DoneSummary:      pay["done_summary"].GetStringValue(),
		Author:           pay["author"].GetStringValue(),
		Requester:        pay["requester"].GetStringValue(),
		CreatedAt:        pay["created_at"].GetStringValue(),
		UpdatedAt:        pay["updated_at"].GetStringValue(),
		Version:          pay["task_version"].GetIntegerValue(),
		Owner:            pay["owner"].GetStringValue(),
		ClaimedAt:        pay["claimed_at"].GetStringValue(),
		LeaseUntil:       pay["lease_until"].GetStringValue(),
		ReviewStatus:     rs,
		ReviewedBy:       pay["reviewed_by"].GetStringValue(),
		ReviewedAt:       pay["reviewed_at"].GetStringValue(),
		ReviewNotes:      pay["review_notes"].GetStringValue(),
		ReadyForLiveBy:   pay["ready_for_live_by"].GetStringValue(),
		ReadyForLiveAt:   pay["ready_for_live_at"].GetStringValue(),
		ReadyForLivePlan: pay["ready_for_live_plan"].GetStringValue(),
		VectorDegraded:   pay["gg_vector_degraded"].GetStringValue() != "",
	}
}
