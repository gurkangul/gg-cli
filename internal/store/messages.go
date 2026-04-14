package store

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/qdrant/go-client/qdrant"
)

type Message struct {
	ID        string
	FromRole  string
	ToRole    string
	Content   string
	Read      bool
	TaskID    string
	CreatedAt string
}

func (c *Client) SendMessage(ctx context.Context, m Message) error {
	if m.ID == "" {
		m.ID = uuid.New().String()
	}
	if m.CreatedAt == "" {
		m.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}

	payload, err := qdrant.TryValueMap(map[string]any{
		"from_role":  m.FromRole,
		"to_role":    m.ToRole,
		"content":    m.Content,
		"read":       false,
		"task_id":    m.TaskID,
		"created_at": m.CreatedAt,
	})
	if err != nil {
		return fmt.Errorf("build payload: %w", err)
	}

	// Messages use a zero vector — they are filtered by role, not searched semantically.
	zeroVec := make([]float32, VectorSize)
	wait := true
	_, err = c.qc.Upsert(ctx, &qdrant.UpsertPoints{
		CollectionName: c.collMessages(),
		Wait:           &wait,
		Points: []*qdrant.PointStruct{
			{
				Id:      qdrant.NewID(m.ID),
				Vectors: qdrant.NewVectors(zeroVec...),
				Payload: payload,
			},
		},
	})
	return err
}

func (c *Client) GetInbox(ctx context.Context, role string) ([]Message, error) {
	conditions := []*qdrant.Condition{
		qdrant.NewMatchBool("read", false),
	}
	if role != "" {
		conditions = append(conditions, qdrant.NewMatchKeyword("to_role", role))
	}

	points, err := c.scrollAll(ctx, &qdrant.ScrollPoints{
		CollectionName: c.collMessages(),
		WithPayload:    qdrant.NewWithPayloadEnable(true),
		Filter:         &qdrant.Filter{Must: conditions},
	})
	if err != nil {
		return nil, err
	}

	messages := make([]Message, 0, len(points))
	for _, p := range points {
		messages = append(messages, messageFromRetrieved(p))
	}
	return messages, nil
}

func (c *Client) MarkMessagesRead(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	pointIDs := make([]*qdrant.PointId, len(ids))
	for i, id := range ids {
		pointIDs[i] = qdrant.NewID(id)
	}

	wait := true
	readVal, _ := qdrant.NewValue(true)
	_, err := c.qc.SetPayload(ctx, &qdrant.SetPayloadPoints{
		CollectionName: c.collMessages(),
		Wait:           &wait,
		Payload: map[string]*qdrant.Value{
			"read": readVal,
		},
		PointsSelector: qdrant.NewPointsSelector(pointIDs...),
	})
	return err
}

func messageFromRetrieved(p *qdrant.RetrievedPoint) Message {
	pay := p.GetPayload()
	return Message{
		ID:        p.GetId().GetUuid(),
		FromRole:  pay["from_role"].GetStringValue(),
		ToRole:    pay["to_role"].GetStringValue(),
		Content:   pay["content"].GetStringValue(),
		Read:      pay["read"].GetBoolValue(),
		TaskID:    pay["task_id"].GetStringValue(),
		CreatedAt: pay["created_at"].GetStringValue(),
	}
}
