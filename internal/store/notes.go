package store

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/gurkangul/gg-cli/internal/brain"
)

// Note is a free-form activity log entry — a timestamped text fragment that
// can be searched semantically. Useful for capturing context, observations,
// or ambient progress that doesn't warrant a decision or task entry.
type Note struct {
	ID            string
	Text          string
	Tags          []string
	TaskID        string
	Author        string // BUG-071: who wrote the note (role/agent) — provenance
	CreatedAt     string
	SemanticScore float32 `json:"semantic_score,omitempty"`
}

func (c *Client) AddNote(ctx context.Context, n Note, vector []float32) (string, error) {
	if n.ID == "" {
		n.ID = uuid.New().String()
	}
	if n.CreatedAt == "" {
		n.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}

	rawPayload := map[string]any{
		"text":       n.Text,
		"tags":       toAnySlice(n.Tags),
		"task_id":    n.TaskID,
		"author":     n.Author,
		"created_at": n.CreatedAt,
	}
	if err := brain.Append(c.dataDir, "notes", n.ID, n.Author, rawPayload); err != nil {
		return "", fmt.Errorf("brain jsonl write: %w", err)
	}
	if len(vector) == 0 {
		return n.ID, semanticVectorMissing(OutboxKindNote, n.ID)
	}

	payload, err := TryValueMap(rawPayload)
	if err != nil {
		return "", fmt.Errorf("build payload: %w", err)
	}

	wait := true
	err = c.vsUpsert(ctx, &UpsertPoints{
		CollectionName: c.collNotes(),
		Wait:           &wait,
		Points: []*PointStruct{
			{
				Id:      NewID(n.ID),
				Vectors: NewVectors(vector...),
				Payload: payload,
			},
		},
	})
	if err != nil {
		return n.ID, &OutboxQueued{Kind: OutboxKindNote, UUID: n.ID, Cause: err}
	}
	return n.ID, nil
}

func (c *Client) ListNotes(ctx context.Context, limit int) ([]Note, error) {
	points, err := c.scrollAll(ctx, &ScrollPoints{
		CollectionName: c.collNotes(),
		WithPayload:    NewWithPayloadEnable(true),
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
	results, err := c.vsQuery(ctx, &QueryPoints{
		CollectionName: c.collNotes(),
		Query:          NewQuery(vector...),
		Limit:          PtrOf(limit),
		WithPayload:    NewWithPayloadEnable(true),
		Filter:         &Filter{Must: []*Condition{nonDegradedVectorCondition()}},
	})
	if err != nil {
		return nil, err
	}
	notes := make([]Note, 0, len(results))
	for _, r := range results {
		n := noteFromPayload(r.GetId().GetUuid(), r.GetPayload())
		n.SemanticScore = r.GetScore()
		notes = append(notes, n)
	}
	return notes, nil
}

func noteFromPayload(id string, pay map[string]*Value) Note {
	return Note{
		ID:        id,
		Text:      pay["text"].GetStringValue(),
		Tags:      extractStringList(pay["tags"]),
		TaskID:    pay["task_id"].GetStringValue(),
		Author:    pay["author"].GetStringValue(),
		CreatedAt: pay["created_at"].GetStringValue(),
	}
}
