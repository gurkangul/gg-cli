package store

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/gurkangul/gg-cli/internal/brain"
	"github.com/qdrant/go-client/qdrant"
)

type Decision struct {
	ID                   string
	Text                 string
	Reason               string
	Status               string   // active|superseded|rejected (default active)
	RejectedAlternatives []string // approaches that were considered and rejected
	Tags                 []string
	TaskID               string
	Author               string // agent role or user that recorded this decision (e.g. "developer")
	CreatedAt            string
	SemanticScore        float32 `json:"semantic_score,omitempty"`
	VectorDegraded       bool    `json:"vector_degraded,omitempty"`
}

// AddDecision writes the decision to .gg/brain/decisions.jsonl first (durable,
// offline-safe), then attempts a Qdrant upsert.  Returns an OutboxQueued error
// when Qdrant is unreachable so callers can surface the queued-for-replay note.
func (c *Client) AddDecision(ctx context.Context, d Decision, vector []float32) error {
	if d.ID == "" {
		d.ID = uuid.New().String()
	}
	if d.CreatedAt == "" {
		d.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	if d.Status == "" {
		d.Status = "active"
	}

	rawPayload := map[string]any{
		"text":                  d.Text,
		"reason":                d.Reason,
		"status":                d.Status,
		"rejected_alternatives": toAnySlice(d.RejectedAlternatives),
		"tags":                  toAnySlice(d.Tags),
		"task_id":               d.TaskID,
		"author":                d.Author,
		"created_at":            d.CreatedAt,
	}

	// AC-1: JSONL write is the primary source of truth.
	if err := brain.Append(c.dataDir, "decisions", d.ID, d.Author, rawPayload); err != nil {
		return fmt.Errorf("brain jsonl write: %w", err)
	}
	if len(vector) == 0 {
		return semanticVectorMissing(OutboxKindDecision, d.ID)
	}

	// AC-2: Qdrant upsert is secondary best-effort.
	qdrantPayload, err := qdrant.TryValueMap(rawPayload)
	if err != nil {
		return fmt.Errorf("build payload: %w", err)
	}
	wait := true
	uErr := c.qdrantUpsert(ctx, &qdrant.UpsertPoints{
		CollectionName: c.collDecisions(),
		Wait:           &wait,
		Points: []*qdrant.PointStruct{
			{
				Id:      qdrant.NewID(d.ID),
				Vectors: qdrant.NewVectors(vector...),
				Payload: qdrantPayload,
			},
		},
	})
	if uErr != nil {
		return &OutboxQueued{Kind: OutboxKindDecision, UUID: d.ID, Cause: uErr}
	}
	return nil
}

// nonDegradedVectorCondition excludes zero-vector records from semantic search.
// Records tagged gg_vector_degraded have a zero vector and produce meaningless
// cosine similarity scores — they must not appear in ranked results.
func nonDegradedVectorCondition() *qdrant.Condition {
	return qdrant.NewIsNull("gg_vector_degraded")
}

// ActiveDecisionsFilter returns the Qdrant filter that restricts results to
// active decisions only (status="active"). Superseded and rejected decisions
// are excluded from default retrieval — they remain queryable with includeAll=true.
func ActiveDecisionsFilter() *qdrant.Filter {
	return &qdrant.Filter{
		Must: []*qdrant.Condition{
			qdrant.NewMatchKeyword("status", "active"),
		},
	}
}

// SearchDecisions performs a semantic search across the decisions collection.
// When includeAll is false (the default for agent consumption), only active
// decisions are returned — superseded and rejected decisions are suppressed so
// agents don't surface stale or overridden context.
func (c *Client) SearchDecisions(ctx context.Context, vector []float32, limit uint64, includeAll bool) ([]Decision, error) {
	req := &qdrant.QueryPoints{
		CollectionName: c.collDecisions(),
		Query:          qdrant.NewQuery(vector...),
		Limit:          qdrant.PtrOf(limit),
		WithPayload:    qdrant.NewWithPayloadEnable(true),
	}
	if !includeAll {
		f := ActiveDecisionsFilter()
		f.Must = append(f.Must, nonDegradedVectorCondition())
		req.Filter = f
	} else {
		req.Filter = &qdrant.Filter{Must: []*qdrant.Condition{nonDegradedVectorCondition()}}
	}
	results, err := c.qdrantQuery(ctx, req)
	if err != nil {
		return nil, err
	}

	decisions := make([]Decision, 0, len(results))
	for _, r := range results {
		d := decisionFromPayload(r.GetId().GetUuid(), r.GetPayload())
		d.SemanticScore = r.GetScore()
		decisions = append(decisions, d)
	}
	return decisions, nil
}

// ListDecisions returns the most recently created decisions, sorted descending
// by created_at. It paginates internally and then trims to limit — Qdrant's
// scroll itself has no time-ordering guarantee.
// When includeAll is false, only active decisions are returned.
func (c *Client) ListDecisions(ctx context.Context, limit int, includeAll bool) ([]Decision, error) {
	req := &qdrant.ScrollPoints{
		CollectionName: c.collDecisions(),
		WithPayload:    qdrant.NewWithPayloadEnable(true),
	}
	if !includeAll {
		req.Filter = ActiveDecisionsFilter()
	}
	points, err := c.scrollAll(ctx, req)
	if err != nil {
		return nil, err
	}
	decisions := make([]Decision, 0, len(points))
	for _, p := range points {
		decisions = append(decisions, decisionFromPayload(p.GetId().GetUuid(), p.GetPayload()))
	}
	sortDecisionsDesc(decisions)
	if limit > 0 && len(decisions) > limit {
		decisions = decisions[:limit]
	}
	return decisions, nil
}

// ListDecisionsByTaskID returns every decision whose TaskID field matches
// the given ID exactly. Semantic near-matches are deliberately excluded —
// callers (notably the decision-capture gate) need structural proof that the
// agent linked the decision on purpose, not merely that the model happened to
// surface something related.
func (c *Client) ListDecisionsByTaskID(ctx context.Context, taskID string) ([]Decision, error) {
	if taskID == "" {
		return nil, nil
	}
	points, err := c.scrollAll(ctx, &qdrant.ScrollPoints{
		CollectionName: c.collDecisions(),
		WithPayload:    qdrant.NewWithPayloadEnable(true),
	})
	if err != nil {
		return nil, err
	}
	var decisions []Decision
	for _, p := range points {
		d := decisionFromPayload(p.GetId().GetUuid(), p.GetPayload())
		if d.TaskID == taskID {
			decisions = append(decisions, d)
		}
	}
	sortDecisionsDesc(decisions)
	return decisions, nil
}

func sortDecisionsDesc(ds []Decision) {
	sort.Slice(ds, func(i, j int) bool {
		return ds[i].CreatedAt > ds[j].CreatedAt // RFC3339 is lexically orderable
	})
}

func decisionFromPayload(id string, pay map[string]*qdrant.Value) Decision {
	status := pay["status"].GetStringValue()
	if status == "" {
		status = "active"
	}
	return Decision{
		ID:                   id,
		Text:                 pay["text"].GetStringValue(),
		Reason:               pay["reason"].GetStringValue(),
		Status:               status,
		RejectedAlternatives: extractStringList(pay["rejected_alternatives"]),
		Tags:                 extractStringList(pay["tags"]),
		TaskID:               pay["task_id"].GetStringValue(),
		Author:               pay["author"].GetStringValue(),
		CreatedAt:            pay["created_at"].GetStringValue(),
		VectorDegraded:       pay["gg_vector_degraded"].GetStringValue() != "",
	}
}

// ValidDecisionStatuses is the allowed set of Decision.Status values.
var ValidDecisionStatuses = map[string]bool{
	"active":     true,
	"superseded": true,
	"rejected":   true,
}

// UpdateDecisionStatus sets the status field on an existing decision point.
func (c *Client) UpdateDecisionStatus(ctx context.Context, decisionID, status string) error {
	if !ValidDecisionStatuses[status] {
		return fmt.Errorf("invalid decision status %q — use active, superseded, or rejected", status)
	}
	payload, err := qdrant.TryValueMap(map[string]any{"status": status})
	if err != nil {
		return fmt.Errorf("build payload: %w", err)
	}
	wait := true
	_, err = c.qc.SetPayload(ctx, &qdrant.SetPayloadPoints{
		CollectionName: c.collDecisions(),
		Wait:           &wait,
		Payload:        payload,
		PointsSelector: qdrant.NewPointsSelector(qdrant.NewID(decisionID)),
	})
	return err
}

func toAnySlice(ss []string) []any {
	if ss == nil {
		return nil
	}
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

func extractStringList(v *qdrant.Value) []string {
	if v == nil {
		return nil
	}
	list := v.GetListValue()
	if list == nil {
		return nil
	}
	var result []string
	for _, item := range list.GetValues() {
		result = append(result, item.GetStringValue())
	}
	return result
}
