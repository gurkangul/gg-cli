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
	ID        string
	Text      string
	Reason    string
	Tags      []string
	TaskID    string
	CreatedAt string
}

func (c *Client) AddDecision(ctx context.Context, d Decision, vector []float32) error {
	if d.ID == "" {
		d.ID = uuid.New().String()
	}
	if d.CreatedAt == "" {
		d.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}

	payload, err := qdrant.TryValueMap(map[string]any{
		"text":       d.Text,
		"reason":     d.Reason,
		"tags":       toAnySlice(d.Tags),
		"task_id":    d.TaskID,
		"created_at": d.CreatedAt,
	})
	if err != nil {
		return fmt.Errorf("build payload: %w", err)
	}

	wait := true
	_, err = c.qc.Upsert(ctx, &qdrant.UpsertPoints{
		CollectionName: CollDecisions,
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
	results, err := c.qc.Query(ctx, &qdrant.QueryPoints{
		CollectionName: CollDecisions,
		Query:          qdrant.NewQuery(vector...),
		Limit:          qdrant.PtrOf(limit),
		WithPayload:    qdrant.NewWithPayloadEnable(true),
	})
	if err != nil {
		return nil, err
	}

	var decisions []Decision
	for _, r := range results {
		decisions = append(decisions, decisionFromPoint(r))
	}
	return decisions, nil
}

// ListDecisions returns the most recently created decisions, sorted descending
// by created_at. It paginates internally and then trims to limit — Qdrant's
// scroll itself has no time-ordering guarantee.
func (c *Client) ListDecisions(ctx context.Context, limit int) ([]Decision, error) {
	points, err := c.scrollAll(ctx, &qdrant.ScrollPoints{
		CollectionName: CollDecisions,
		WithPayload:    qdrant.NewWithPayloadEnable(true),
	})
	if err != nil {
		return nil, err
	}
	decisions := make([]Decision, 0, len(points))
	for _, p := range points {
		decisions = append(decisions, decisionFromRetrieved(p))
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

func decisionFromPoint(p *qdrant.ScoredPoint) Decision {
	pay := p.GetPayload()
	return Decision{
		ID:        p.GetId().GetUuid(),
		Text:      pay["text"].GetStringValue(),
		Reason:    pay["reason"].GetStringValue(),
		Tags:      extractStringList(pay["tags"]),
		TaskID:    pay["task_id"].GetStringValue(),
		CreatedAt: pay["created_at"].GetStringValue(),
	}
}

func decisionFromRetrieved(p *qdrant.RetrievedPoint) Decision {
	pay := p.GetPayload()
	return Decision{
		ID:        p.GetId().GetUuid(),
		Text:      pay["text"].GetStringValue(),
		Reason:    pay["reason"].GetStringValue(),
		Tags:      extractStringList(pay["tags"]),
		TaskID:    pay["task_id"].GetStringValue(),
		CreatedAt: pay["created_at"].GetStringValue(),
	}
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
