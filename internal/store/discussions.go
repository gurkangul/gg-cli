package store

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/qdrant/go-client/qdrant"
)

// discIDNamespace seeds the deterministic UUID for a discussion point, mirroring
// the task pattern so concurrent opens with the same DISC-ID collapse safely.
var discIDNamespace = uuid.MustParse("c0c0c0c0-1a5c-4d0d-bab0-000000000002")

var discIDRegex = regexp.MustCompile(`^DISC-\d{3,}$`)

// Discussion represents an open topic that has been surfaced but has not yet
// been concluded as a decision/rejection/task. Every open discussion MUST
// eventually be resolved (linked to an artifact) or dismissed (marked
// irrelevant). This prevents conversations from lingering forever.
type Discussion struct {
	ID           string
	Topic        string
	Detail       string
	Status       string // "open" | "resolved" | "dismissed"
	ResolvedVia  string // "decision" | "task" | "rejection" (when resolved)
	ResolvedNote string // summary of how it was resolved
	DismissNote  string // why it was dismissed (if applicable)
	Tags         []string
	CreatedAt    string
	UpdatedAt    string
}

func pointUUIDForDiscID(id string) string {
	return uuid.NewSHA1(discIDNamespace, []byte(id)).String()
}

func (c *Client) maxDiscIDNumber(ctx context.Context) (int, error) {
	points, err := c.scrollAll(ctx, &qdrant.ScrollPoints{
		CollectionName: c.collDiscussions(),
		Limit:          qdrant.PtrOf(uint32(1000)),
		WithPayload:    qdrant.NewWithPayloadInclude("disc_id"),
	})
	if err != nil {
		return 0, fmt.Errorf("scan discussions collection: %w", err)
	}
	maxNum := 0
	for _, p := range points {
		id := p.GetPayload()["disc_id"].GetStringValue()
		if n, err := ParseDiscID(id); err == nil && n > maxNum {
			maxNum = n
		}
	}
	return maxNum, nil
}

// OpenDiscussion allocates a new DISC-NNN and persists the topic in status=open.
func (c *Client) OpenDiscussion(ctx context.Context, d Discussion, vector []float32) (string, error) {
	id, err := c.allocDiscID(ctx)
	if err != nil {
		return "", err
	}
	d.ID = id
	d.Status = "open"
	now := time.Now().UTC().Format(time.RFC3339)
	d.CreatedAt = now
	d.UpdatedAt = now

	payload, err := qdrant.TryValueMap(map[string]any{
		"disc_id":       d.ID,
		"topic":         d.Topic,
		"detail":        d.Detail,
		"status":        d.Status,
		"resolved_via":  "",
		"resolved_note": "",
		"dismiss_note":  "",
		"tags":          toAnySlice(d.Tags),
		"created_at":    d.CreatedAt,
		"updated_at":    d.UpdatedAt,
	})
	if err != nil {
		return "", fmt.Errorf("build payload: %w", err)
	}

	wait := true
	_, err = c.qc.Upsert(ctx, &qdrant.UpsertPoints{
		CollectionName: c.collDiscussions(),
		Wait:           &wait,
		Points: []*qdrant.PointStruct{
			{
				Id:      qdrant.NewID(pointUUIDForDiscID(d.ID)),
				Vectors: qdrant.NewVectors(vector...),
				Payload: payload,
			},
		},
	})
	if err != nil {
		return "", err
	}
	return d.ID, nil
}

func (c *Client) GetDiscussion(ctx context.Context, discID string) (*Discussion, error) {
	points, err := c.qc.Get(ctx, &qdrant.GetPoints{
		CollectionName: c.collDiscussions(),
		Ids:            []*qdrant.PointId{qdrant.NewID(pointUUIDForDiscID(discID))},
		WithPayload:    qdrant.NewWithPayloadEnable(true),
	})
	if err != nil {
		return nil, err
	}
	if len(points) == 0 {
		return nil, fmt.Errorf("discussion %s not found", discID)
	}
	d := discussionFromPayload(points[0].GetPayload())
	return &d, nil
}

func (c *Client) ListDiscussions(ctx context.Context, statusFilter string) ([]Discussion, error) {
	req := &qdrant.ScrollPoints{
		CollectionName: c.collDiscussions(),
		WithPayload:    qdrant.NewWithPayloadEnable(true),
	}
	if statusFilter != "" {
		req.Filter = &qdrant.Filter{
			Must: []*qdrant.Condition{qdrant.NewMatchKeyword("status", statusFilter)},
		}
	}
	points, err := c.scrollAll(ctx, req)
	if err != nil {
		return nil, err
	}
	discussions := make([]Discussion, 0, len(points))
	for _, p := range points {
		discussions = append(discussions, discussionFromPayload(p.GetPayload()))
	}
	sort.Slice(discussions, func(i, j int) bool {
		ni, _ := ParseDiscID(discussions[i].ID)
		nj, _ := ParseDiscID(discussions[j].ID)
		return ni < nj
	})
	return discussions, nil
}

// ResolveDiscussion closes a discussion by linking it to a decision, task, or
// rejection. The caller supplies a short summary of the conclusion.
func (c *Client) ResolveDiscussion(ctx context.Context, discID, via, note string) error {
	return c.closeDiscussion(ctx, discID, "resolved", via, note, "")
}

// DismissDiscussion closes a discussion without a concrete artifact — used when
// the topic becomes irrelevant, stale, or superseded.
func (c *Client) DismissDiscussion(ctx context.Context, discID, reason string) error {
	return c.closeDiscussion(ctx, discID, "dismissed", "", "", reason)
}

func (c *Client) closeDiscussion(ctx context.Context, discID, status, via, note, dismissNote string) error {
	pointID := qdrant.NewID(pointUUIDForDiscID(discID))
	existing, err := c.qc.Get(ctx, &qdrant.GetPoints{
		CollectionName: c.collDiscussions(),
		Ids:            []*qdrant.PointId{pointID},
		WithPayload:    qdrant.NewWithPayloadInclude("disc_id", "status"),
	})
	if err != nil {
		return err
	}
	if len(existing) == 0 {
		return fmt.Errorf("discussion %s not found", discID)
	}
	if s := existing[0].GetPayload()["status"].GetStringValue(); s != "open" {
		return fmt.Errorf("discussion %s is already %s", discID, s)
	}

	statusVal, _ := qdrant.NewValue(status)
	viaVal, _ := qdrant.NewValue(via)
	noteVal, _ := qdrant.NewValue(note)
	dismissVal, _ := qdrant.NewValue(dismissNote)
	updatedVal, _ := qdrant.NewValue(time.Now().UTC().Format(time.RFC3339))

	payload := map[string]*qdrant.Value{
		"status":        statusVal,
		"resolved_via":  viaVal,
		"resolved_note": noteVal,
		"dismiss_note":  dismissVal,
		"updated_at":    updatedVal,
	}

	wait := true
	_, err = c.qc.SetPayload(ctx, &qdrant.SetPayloadPoints{
		CollectionName: c.collDiscussions(),
		Wait:           &wait,
		Payload:        payload,
		PointsSelector: qdrant.NewPointsSelector(pointID),
	})
	return err
}

func (c *Client) CountOpenDiscussions(ctx context.Context) (uint64, error) {
	return c.qc.Count(ctx, &qdrant.CountPoints{
		CollectionName: c.collDiscussions(),
		Filter: &qdrant.Filter{
			Must: []*qdrant.Condition{qdrant.NewMatchKeyword("status", "open")},
		},
	})
}

func discussionFromPayload(pay map[string]*qdrant.Value) Discussion {
	return Discussion{
		ID:           pay["disc_id"].GetStringValue(),
		Topic:        pay["topic"].GetStringValue(),
		Detail:       pay["detail"].GetStringValue(),
		Status:       pay["status"].GetStringValue(),
		ResolvedVia:  pay["resolved_via"].GetStringValue(),
		ResolvedNote: pay["resolved_note"].GetStringValue(),
		DismissNote:  pay["dismiss_note"].GetStringValue(),
		Tags:         extractStringList(pay["tags"]),
		CreatedAt:    pay["created_at"].GetStringValue(),
		UpdatedAt:    pay["updated_at"].GetStringValue(),
	}
}

// ParseDiscID extracts the numeric suffix from a discussion ID like "DISC-001".
func ParseDiscID(id string) (int, error) {
	if !discIDRegex.MatchString(id) {
		return 0, fmt.Errorf("invalid discussion ID %q (expected DISC-NNN)", id)
	}
	return strconv.Atoi(id[5:])
}
