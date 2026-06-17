package store

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/gurkangul/gg-cli/internal/brain"
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
	Evidence             string // BUG-071: how this was verified (commands/smoke/source) — empty = unverified
	Pinned               bool   // TASK-469: surfaced first in overview regardless of age (important-old won't sink)
	CreatedAt            string
	SemanticScore        float32 `json:"semantic_score,omitempty"`
	VectorDegraded       bool    `json:"vector_degraded,omitempty"`
}

// AddDecision writes the decision to .gg/brain/decisions.jsonl first (durable,
// offline-safe), then attempts a vector-store upsert.  Returns an OutboxQueued error
// when the vector store is unreachable so callers can surface the queued-for-replay note.
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
		"evidence":              d.Evidence,
		"pinned":                d.Pinned,
		"created_at":            d.CreatedAt,
		"version":               int64(1),
	}

	// AC-1: JSONL write is the primary source of truth.
	if err := brain.Append(c.dataDir, "decisions", d.ID, d.Author, rawPayload); err != nil {
		return fmt.Errorf("brain jsonl write: %w", err)
	}
	if len(vector) == 0 {
		return semanticVectorMissing(OutboxKindDecision, d.ID)
	}

	// AC-2: vector upsert is secondary best-effort.
	vecPayload, err := TryValueMap(rawPayload)
	if err != nil {
		return fmt.Errorf("build payload: %w", err)
	}
	wait := true
	uErr := c.vsUpsert(ctx, &UpsertPoints{
		CollectionName: c.collDecisions(),
		Wait:           &wait,
		Points: []*PointStruct{
			{
				Id:      NewID(d.ID),
				Vectors: NewVectors(vector...),
				Payload: vecPayload,
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
//
// BUG-085: this MUST use is_empty, not is_null. Normal records never set the
// gg_vector_degraded key at all, and the vector store's is_null matches only keys that
// exist AND are explicitly null — so is_null excluded EVERY record and made all
// Search* queries return zero results. is_empty matches missing/null/empty, so
// it keeps normal records and drops only the explicitly-marked degraded ones.
func nonDegradedVectorCondition() *Condition {
	return NewIsEmpty("gg_vector_degraded")
}

// ActiveDecisionsFilter returns the payload filter that restricts results to
// active decisions only (status="active"). Superseded and rejected decisions
// are excluded from default retrieval — they remain queryable with includeAll=true.
func ActiveDecisionsFilter() *Filter {
	return &Filter{
		Must: []*Condition{
			NewMatchKeyword("status", "active"),
		},
	}
}

// SearchDecisions performs a semantic search across the decisions collection.
// When includeAll is false (the default for agent consumption), only active
// decisions are returned — superseded and rejected decisions are suppressed so
// agents don't surface stale or overridden context.
func (c *Client) SearchDecisions(ctx context.Context, vector []float32, limit uint64, includeAll bool) ([]Decision, error) {
	req := &QueryPoints{
		CollectionName: c.collDecisions(),
		Query:          NewQuery(vector...),
		Limit:          PtrOf(limit),
		WithPayload:    NewWithPayloadEnable(true),
	}
	if !includeAll {
		f := ActiveDecisionsFilter()
		f.Must = append(f.Must, nonDegradedVectorCondition())
		req.Filter = f
	} else {
		req.Filter = &Filter{Must: []*Condition{nonDegradedVectorCondition()}}
	}
	results, err := c.vsQuery(ctx, req)
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
// by created_at. It paginates internally and then trims to limit — the vector store's
// scroll itself has no time-ordering guarantee.
// When includeAll is false, only active decisions are returned.
func (c *Client) ListDecisions(ctx context.Context, limit int, includeAll bool) ([]Decision, error) {
	req := &ScrollPoints{
		CollectionName: c.collDecisions(),
		WithPayload:    NewWithPayloadEnable(true),
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

// ListPinnedDecisions returns active, pinned decisions (TASK-469). The project
// overview surfaces these first so important decisions never sink under recency.
func (c *Client) ListPinnedDecisions(ctx context.Context) ([]Decision, error) {
	points, err := c.scrollAll(ctx, &ScrollPoints{
		CollectionName: c.collDecisions(),
		WithPayload:    NewWithPayloadEnable(true),
		Filter: &Filter{Must: []*Condition{
			NewMatchKeyword("status", "active"),
			NewMatchBool("pinned", true),
		}},
	})
	if err != nil {
		return nil, err
	}
	decisions := make([]Decision, 0, len(points))
	for _, p := range points {
		decisions = append(decisions, decisionFromPayload(p.GetId().GetUuid(), p.GetPayload()))
	}
	sortDecisionsDesc(decisions)
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
	points, err := c.scrollAll(ctx, &ScrollPoints{
		CollectionName: c.collDecisions(),
		WithPayload:    NewWithPayloadEnable(true),
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

func decisionFromPayload(id string, pay map[string]*Value) Decision {
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
		Evidence:             pay["evidence"].GetStringValue(),
		Pinned:               pay["pinned"].GetBoolValue(),
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

// UpdateDecisionStatus sets the status field on an existing decision.
//
// JSONL-first with version/CAS (BUG-062/063): the status change is appended to
// .gg/brain/decisions.jsonl as a new full-payload line under an optimistic
// version guard, then mirrored to the vector store. A concurrent writer that advanced the
// version first yields ErrMutationConflict and writes nothing. The decision's
// point UUID equals its decision ID (see AddDecision).
func (c *Client) UpdateDecisionStatus(ctx context.Context, decisionID, status string) error {
	if !ValidDecisionStatuses[status] {
		return fmt.Errorf("invalid decision status %q — use active, superseded, or rejected", status)
	}
	err := c.applyBrainMutation(ctx, "decisions", c.collDecisions(), OutboxKindDecision, decisionID, "", func(raw map[string]any) error {
		raw["status"] = status
		return nil
	})
	if errors.Is(err, ErrRecordNotFound) {
		return fmt.Errorf("decision %s not found", decisionID)
	}
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

func extractStringList(v *Value) []string {
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
