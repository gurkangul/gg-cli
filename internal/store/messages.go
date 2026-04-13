package store

import (
	"context"
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

func (c *Client) SendMessage(ctx context.Context, m Message, vector []float32) error {
	if m.ID == "" {
		m.ID = uuid.New().String()
	}
	if m.CreatedAt == "" {
		m.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}

	payload := map[string]any{
		"from_role":  m.FromRole,
		"to_role":    m.ToRole,
		"content":    m.Content,
		"read":       false,
		"task_id":    m.TaskID,
		"created_at": m.CreatedAt,
	}

	wait := true
	_, err := c.qc.Upsert(ctx, &qdrant.UpsertPoints{
		CollectionName: CollMessages,
		Wait:           &wait,
		Points: []*qdrant.PointStruct{
			{
				Id:      qdrant.NewID(m.ID),
				Vectors: qdrant.NewVectors(vector...),
				Payload: qdrant.NewValueMap(payload),
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

	points, err := c.qc.Scroll(ctx, &qdrant.ScrollPoints{
		CollectionName: CollMessages,
		Limit:          qdrant.PtrOf(uint32(100)),
		WithPayload:    qdrant.NewWithPayloadEnable(true),
		Filter: &qdrant.Filter{
			Must: conditions,
		},
	})
	if err != nil {
		return nil, err
	}

	var messages []Message
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
		CollectionName: CollMessages,
		Wait:           &wait,
		Payload: map[string]*qdrant.Value{
			"read": readVal,
		},
		PointsSelector: qdrant.NewPointsSelector(pointIDs...),
	})
	return err
}

func (c *Client) CountUnreadMessages(ctx context.Context, role string) (uint64, error) {
	conditions := []*qdrant.Condition{
		qdrant.NewMatchBool("read", false),
	}
	if role != "" {
		conditions = append(conditions, qdrant.NewMatchKeyword("to_role", role))
	}
	return c.qc.Count(ctx, &qdrant.CountPoints{
		CollectionName: CollMessages,
		Filter: &qdrant.Filter{
			Must: conditions,
		},
	})
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
