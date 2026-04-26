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

type Rejection struct {
	ID        string
	Approach  string
	Reason    string
	Tags      []string
	TaskID    string
	Author    string // agent role or user that recorded this rejection
	CreatedAt string
}

// AddRejection writes the rejection to .gg/brain/rejections.jsonl first
// (AC-1: durable, offline-safe), then attempts a Qdrant upsert (AC-2).
func (c *Client) AddRejection(ctx context.Context, r Rejection, vector []float32) error {
	if r.ID == "" {
		r.ID = uuid.New().String()
	}
	if r.CreatedAt == "" {
		r.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}

	rawPayload := map[string]any{
		"approach":   r.Approach,
		"reason":     r.Reason,
		"tags":       toAnySlice(r.Tags),
		"task_id":    r.TaskID,
		"author":     r.Author,
		"created_at": r.CreatedAt,
	}

	// AC-1: JSONL write first.
	if err := brain.Append(c.dataDir, "rejections", r.ID, r.Author, rawPayload); err != nil {
		return fmt.Errorf("brain jsonl write: %w", err)
	}

	// AC-2: Qdrant secondary best-effort.
	qdrantPayload, err := qdrant.TryValueMap(rawPayload)
	if err != nil {
		return fmt.Errorf("build payload: %w", err)
	}
	wait := true
	uErr := c.qdrantUpsert(ctx, &qdrant.UpsertPoints{
		CollectionName: c.collRejections(),
		Wait:           &wait,
		Points: []*qdrant.PointStruct{
			{
				Id:      qdrant.NewID(r.ID),
				Vectors: qdrant.NewVectors(vector...),
				Payload: qdrantPayload,
			},
		},
	})
	if uErr != nil {
		return &OutboxQueued{Kind: OutboxKindRejection, UUID: r.ID, Cause: uErr}
	}
	return nil
}

func (c *Client) SearchRejections(ctx context.Context, vector []float32, limit uint64) ([]Rejection, error) {
	results, err := c.qdrantQuery(ctx, &qdrant.QueryPoints{
		CollectionName: c.collRejections(),
		Query:          qdrant.NewQuery(vector...),
		Limit:          qdrant.PtrOf(limit),
		WithPayload:    qdrant.NewWithPayloadEnable(true),
	})
	if err != nil {
		return nil, err
	}

	rejections := make([]Rejection, 0, len(results))
	for _, r := range results {
		rejections = append(rejections, rejectionFromPayload(r.GetId().GetUuid(), r.GetPayload()))
	}
	return rejections, nil
}

// ListRejections returns rejections sorted by created_at descending, trimmed to limit.
func (c *Client) ListRejections(ctx context.Context, limit int) ([]Rejection, error) {
	points, err := c.scrollAll(ctx, &qdrant.ScrollPoints{
		CollectionName: c.collRejections(),
		WithPayload:    qdrant.NewWithPayloadEnable(true),
	})
	if err != nil {
		return nil, err
	}
	rejections := make([]Rejection, 0, len(points))
	for _, p := range points {
		rejections = append(rejections, rejectionFromPayload(p.GetId().GetUuid(), p.GetPayload()))
	}
	sort.Slice(rejections, func(i, j int) bool {
		return rejections[i].CreatedAt > rejections[j].CreatedAt
	})
	if limit > 0 && len(rejections) > limit {
		rejections = rejections[:limit]
	}
	return rejections, nil
}

func rejectionFromPayload(id string, pay map[string]*qdrant.Value) Rejection {
	return Rejection{
		ID:        id,
		Approach:  pay["approach"].GetStringValue(),
		Reason:    pay["reason"].GetStringValue(),
		Tags:      extractStringList(pay["tags"]),
		TaskID:    pay["task_id"].GetStringValue(),
		Author:    pay["author"].GetStringValue(),
		CreatedAt: pay["created_at"].GetStringValue(),
	}
}
