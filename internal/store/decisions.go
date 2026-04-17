package store

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
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
}

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

	payload, err := qdrant.TryValueMap(map[string]any{
		"text":                  d.Text,
		"reason":                d.Reason,
		"status":                d.Status,
		"rejected_alternatives": toAnySlice(d.RejectedAlternatives),
		"tags":                  toAnySlice(d.Tags),
		"task_id":               d.TaskID,
		"author":                d.Author,
		"created_at":            d.CreatedAt,
	})
	if err != nil {
		return fmt.Errorf("build payload: %w", err)
	}

	wait := true
	err = c.qdrantUpsert(ctx, &qdrant.UpsertPoints{
		CollectionName: c.collDecisions(),
		Wait:           &wait,
		Points: []*qdrant.PointStruct{
			{
				Id:      qdrant.NewID(d.ID),
				Vectors: qdrant.NewVectors(vector...),
				Payload: payload,
			},
		},
	})
	return err
}

func (c *Client) SearchDecisions(ctx context.Context, vector []float32, limit uint64) ([]Decision, error) {
	results, err := c.qdrantQuery(ctx, &qdrant.QueryPoints{
		CollectionName: c.collDecisions(),
		Query:          qdrant.NewQuery(vector...),
		Limit:          qdrant.PtrOf(limit),
		WithPayload:    qdrant.NewWithPayloadEnable(true),
	})
	if err != nil {
		return nil, err
	}

	decisions := make([]Decision, 0, len(results))
	for _, r := range results {
		decisions = append(decisions, decisionFromPayload(r.GetId().GetUuid(), r.GetPayload()))
	}
	return decisions, nil
}

// ListDecisions returns the most recently created decisions, sorted descending
// by created_at. It paginates internally and then trims to limit — Qdrant's
// scroll itself has no time-ordering guarantee.
func (c *Client) ListDecisions(ctx context.Context, limit int) ([]Decision, error) {
	points, err := c.scrollAll(ctx, &qdrant.ScrollPoints{
		CollectionName: c.collDecisions(),
		WithPayload:    qdrant.NewWithPayloadEnable(true),
	})
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
