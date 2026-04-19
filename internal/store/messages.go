package store

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/qdrant/go-client/qdrant"
)

// Audience controls inbox visibility. "agents" = agent-to-agent only (filtered
// from human inbox by default). "human" = human-visible only. "all" = everyone
// (default, backward-compatible).
type Message struct {
	ID        string
	FromRole  string
	ToRole    string
	Content   string
	Audience  string // "all" | "human" | "agents"
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

	audience := m.Audience
	if audience == "" {
		audience = "all"
	}
	payload, err := qdrant.TryValueMap(map[string]any{
		"from_role":  m.FromRole,
		"to_role":    m.ToRole,
		"content":    m.Content,
		"audience":   audience,
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
	err = c.qdrantUpsert(ctx, &qdrant.UpsertPoints{
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

// GetInbox returns unread messages. When humanOnly is true, messages with
// audience="agents" are excluded (human-facing inbox view).
func (c *Client) GetInbox(ctx context.Context, role string, humanOnly bool) ([]Message, error) {
	conditions := []*qdrant.Condition{
		qdrant.NewMatchBool("read", false),
	}
	if role != "" {
		conditions = append(conditions, qdrant.NewMatchKeyword("to_role", role))
	}
	filter := &qdrant.Filter{Must: conditions}
	if humanOnly {
		filter.MustNot = []*qdrant.Condition{
			qdrant.NewMatchKeyword("audience", "agents"),
		}
	}

	points, err := c.scrollAll(ctx, &qdrant.ScrollPoints{
		CollectionName: c.collMessages(),
		WithPayload:    qdrant.NewWithPayloadEnable(true),
		Filter:         filter,
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

// DismissAll marks all unread messages (optionally filtered by recipient role)
// as read. Returns the count of dismissed messages.
func (c *Client) DismissAll(ctx context.Context, role string) (int, error) {
	msgs, err := c.GetInbox(ctx, role, false)
	if err != nil {
		return 0, err
	}
	if len(msgs) == 0 {
		return 0, nil
	}
	ids := make([]string, len(msgs))
	for i, m := range msgs {
		ids[i] = m.ID
	}
	if err := c.MarkMessagesRead(ctx, ids); err != nil {
		return 0, err
	}
	return len(msgs), nil
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
	audience := pay["audience"].GetStringValue()
	if audience == "" {
		audience = "all"
	}
	return Message{
		ID:        p.GetId().GetUuid(),
		FromRole:  pay["from_role"].GetStringValue(),
		ToRole:    pay["to_role"].GetStringValue(),
		Content:   pay["content"].GetStringValue(),
		Audience:  audience,
		Read:      pay["read"].GetBoolValue(),
		TaskID:    pay["task_id"].GetStringValue(),
		CreatedAt: pay["created_at"].GetStringValue(),
	}
}
