package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/gurkangul/gg-cli/internal/brain"
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
	Audience  string   // "all" | "human" | "agents"
	Read      bool     // legacy global dismiss (read by everyone)
	ReadBy    []string // BUG-082: per-recipient read set, keyed by reader identity
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
	rawPayload := map[string]any{
		"from_role":  m.FromRole,
		"to_role":    m.ToRole,
		"content":    m.Content,
		"audience":   audience,
		"read":       false,
		"read_by":    toAnySlice(nil),
		"task_id":    m.TaskID,
		"created_at": m.CreatedAt,
		"version":    int64(1),
	}
	if err := brain.Append(c.dataDir, "messages", m.ID, m.FromRole, rawPayload); err != nil {
		return fmt.Errorf("brain jsonl write: %w", err)
	}

	payload, err := qdrant.TryValueMap(rawPayload)
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
	if err != nil {
		return &OutboxQueued{Kind: OutboxKindMessage, UUID: m.ID, Cause: err}
	}
	return nil
}

// GetInbox returns messages unread by reader. When humanOnly is true, messages
// with audience="agents" are excluded (human-facing inbox view).
//
// BUG-082: read-state is per-recipient. A message is hidden from reader only
// when it carries the legacy global read=true flag OR reader appears in its
// per-recipient read_by set. reader="" means an anonymous/preview view (status,
// next) that never consumes another agent's message — it sees everything not
// globally dismissed.
func (c *Client) GetInbox(ctx context.Context, role string, humanOnly bool, reader string) ([]Message, error) {
	conditions := []*qdrant.Condition{}
	if role != "" {
		conditions = append(conditions, qdrant.NewMatchKeyword("to_role", role))
	}
	filter := &qdrant.Filter{Must: conditions}
	// Exclude legacy globally-dismissed messages and, for an identified reader,
	// messages already in that reader's read_by set.
	filter.MustNot = []*qdrant.Condition{qdrant.NewMatchBool("read", true)}
	if reader != "" {
		filter.MustNot = append(filter.MustNot, qdrant.NewMatchKeyword("read_by", reader))
	}
	if humanOnly {
		filter.MustNot = append(filter.MustNot, qdrant.NewMatchKeyword("audience", "agents"))
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
// ListMessagesSince returns all messages (read and unread) created at or after
// the given time. Filtering is done in memory because created_at is a string
// payload and Qdrant range filters require numeric fields.
func (c *Client) ListMessagesSince(ctx context.Context, since time.Time) ([]Message, error) {
	points, err := c.scrollAll(ctx, &qdrant.ScrollPoints{
		CollectionName: c.collMessages(),
		WithPayload:    qdrant.NewWithPayloadEnable(true),
	})
	if err != nil {
		return nil, err
	}
	var out []Message
	for _, p := range points {
		m := messageFromRetrieved(p)
		t, err := time.Parse(time.RFC3339, m.CreatedAt)
		if err != nil {
			continue
		}
		if !t.Before(since) {
			out = append(out, m)
		}
	}
	return out, nil
}

func (c *Client) DismissAll(ctx context.Context, role, reader string) (int, error) {
	msgs, err := c.GetInbox(ctx, role, false, reader)
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
	if err := c.MarkMessagesRead(ctx, ids, reader); err != nil {
		return 0, err
	}
	return len(msgs), nil
}

// MarkMessagesRead marks messages read for reader.
//
// BUG-082: when reader is identified (a GG_AGENT), the read is recorded in the
// per-recipient read_by set (JSONL-first via applyBrainMutation) so it does NOT
// consume the message for other recipients. An empty reader falls back to the
// legacy global read=true flag (anonymous/operator dismiss visible to everyone).
func (c *Client) MarkMessagesRead(ctx context.Context, ids []string, reader string) error {
	if len(ids) == 0 {
		return nil
	}
	if reader == "" {
		return c.markMessagesGloballyRead(ctx, ids)
	}
	for _, id := range ids {
		err := c.applyBrainMutation(ctx, "messages", c.collMessages(), OutboxKindMessage, id, "", func(raw map[string]any) error {
			readers := anyStringList(raw["read_by"])
			for _, r := range readers {
				if r == reader {
					return errMutationNoop
				}
			}
			raw["read_by"] = toAnySlice(append(readers, reader))
			return nil
		})
		switch {
		case errors.Is(err, errMutationNoop), errors.Is(err, ErrRecordNotFound):
			// Already read by this reader, or message only in Qdrant with no
			// brain entry — nothing to persist.
			continue
		case err != nil:
			return err
		}
	}
	return nil
}

// errMutationNoop lets an apply func signal "no change needed" so
// applyBrainMutation can be skipped without writing a redundant JSONL line.
var errMutationNoop = errors.New("mutation noop")

func (c *Client) markMessagesGloballyRead(ctx context.Context, ids []string) error {
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
		ReadBy:    extractStringList(pay["read_by"]),
		TaskID:    pay["task_id"].GetStringValue(),
		CreatedAt: pay["created_at"].GetStringValue(),
	}
}
