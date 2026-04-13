package store

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/qdrant/go-client/qdrant"
)

type Rejection struct {
	ID        string
	Approach  string
	Reason    string
	TaskID    string
	CreatedAt string
}

func (c *Client) AddRejection(ctx context.Context, r Rejection, vector []float32) error {
	if r.ID == "" {
		r.ID = uuid.New().String()
	}
	if r.CreatedAt == "" {
		r.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}

	payload, err := qdrant.TryValueMap(map[string]any{
		"approach":   r.Approach,
		"reason":     r.Reason,
		"task_id":    r.TaskID,
		"created_at": r.CreatedAt,
	})
	if err != nil {
		return fmt.Errorf("build payload: %w", err)
	}

	wait := true
	_, err = c.qc.Upsert(ctx, &qdrant.UpsertPoints{
		CollectionName: CollRejections,
		Wait:           &wait,
		Points: []*qdrant.PointStruct{
			{
				Id:      qdrant.NewID(r.ID),
				Vectors: qdrant.NewVectors(vector...),
				Payload: payload,
			},
		},
	})
	return err
}

func (c *Client) SearchRejections(ctx context.Context, vector []float32, limit uint64) ([]Rejection, error) {
	results, err := c.qc.Query(ctx, &qdrant.QueryPoints{
		CollectionName: CollRejections,
		Query:          qdrant.NewQuery(vector...),
		Limit:          qdrant.PtrOf(limit),
		WithPayload:    qdrant.NewWithPayloadEnable(true),
	})
	if err != nil {
		return nil, err
	}

	var rejections []Rejection
	for _, r := range results {
		pay := r.GetPayload()
		rejections = append(rejections, Rejection{
			ID:        r.GetId().GetUuid(),
			Approach:  pay["approach"].GetStringValue(),
			Reason:    pay["reason"].GetStringValue(),
			TaskID:    pay["task_id"].GetStringValue(),
			CreatedAt: pay["created_at"].GetStringValue(),
		})
	}
	return rejections, nil
}

func (c *Client) ListRejections(ctx context.Context, limit uint32) ([]Rejection, error) {
	points, err := c.qc.Scroll(ctx, &qdrant.ScrollPoints{
		CollectionName: CollRejections,
		Limit:          qdrant.PtrOf(limit),
		WithPayload:    qdrant.NewWithPayloadEnable(true),
	})
	if err != nil {
		return nil, err
	}
	var rejections []Rejection
	for _, p := range points {
		pay := p.GetPayload()
		rejections = append(rejections, Rejection{
			ID:        p.GetId().GetUuid(),
			Approach:  pay["approach"].GetStringValue(),
			Reason:    pay["reason"].GetStringValue(),
			TaskID:    pay["task_id"].GetStringValue(),
			CreatedAt: pay["created_at"].GetStringValue(),
		})
	}
	return rejections, nil
}
