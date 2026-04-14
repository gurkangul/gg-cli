package store

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/qdrant/go-client/qdrant"
)

// Note is a free-form activity log entry — a timestamped text fragment that
// can be searched semantically. Useful for capturing context, observations,
// or ambient progress that doesn't warrant a decision or task entry.
type Note struct {
	ID        string
	Text      string
	Tags      []string
	TaskID    string
	CreatedAt string
}

func (c *Client) AddNote(ctx context.Context, n Note, vector []float32) (string, error) {
	if n.ID == "" {
		n.ID = uuid.New().String()
	}
	if n.CreatedAt == "" {
		n.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}

	payload, err := qdrant.TryValueMap(map[string]any{
		"text":       n.Text,
		"tags":       toAnySlice(n.Tags),
		"task_id":    n.TaskID,
		"created_at": n.CreatedAt,
	})
	if err != nil {
		return "", fmt.Errorf("build payload: %w", err)
	}

	wait := true
	err = c.qdrantUpsert(ctx, &qdrant.UpsertPoints{
		CollectionName: c.collNotes(),
		Wait:           &wait,
		Points: []*qdrant.PointStruct{
			{
				Id:      qdrant.NewID(n.ID),
				Vectors: qdrant.NewVectors(vector...),
				Payload: payload,
			},
		},
	})
	if err != nil {
		return "", err
	}
	return n.ID, nil
}

func (c *Client) ListNotes(ctx context.Context, limit int) ([]Note, error) {
	points, err := c.scrollAll(ctx, &qdrant.ScrollPoints{
		CollectionName: c.collNotes(),
		WithPayload:    qdrant.NewWithPayloadEnable(true),
	})
	if err != nil {
		return nil, err
	}
	notes := make([]Note, 0, len(points))
	for _, p := range points {
		notes = append(notes, noteFromPayload(p.GetId().GetUuid(), p.GetPayload()))
	}
	// Sort newest first.
	sort.Slice(notes, func(i, j int) bool {
		return notes[i].CreatedAt > notes[j].CreatedAt
	})
	if limit > 0 && len(notes) > limit {
		notes = notes[:limit]
	}
	return notes, nil
}

func (c *Client) SearchNotes(ctx context.Context, vector []float32, limit uint64) ([]Note, error) {
	results, err := c.qdrantQuery(ctx, &qdrant.QueryPoints{
		CollectionName: c.collNotes(),
		Query:          qdrant.NewQuery(vector...),
		Limit:          qdrant.PtrOf(limit),
		WithPayload:    qdrant.NewWithPayloadEnable(true),
	})
	if err != nil {
		return nil, err
	}
	notes := make([]Note, 0, len(results))
	for _, r := range results {
		notes = append(notes, noteFromPayload(r.GetId().GetUuid(), r.GetPayload()))
	}
	return notes, nil
}

func noteFromPayload(id string, pay map[string]*qdrant.Value) Note {
	return Note{
		ID:        id,
		Text:      pay["text"].GetStringValue(),
		Tags:      extractStringList(pay["tags"]),
		TaskID:    pay["task_id"].GetStringValue(),
		CreatedAt: pay["created_at"].GetStringValue(),
	}
}
