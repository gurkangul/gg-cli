package store

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/qdrant/go-client/qdrant"
)

// Turn is one deliberation entry in a discussion transcript.
type Turn struct {
	By   string // agent or user name
	Text string // the contribution text
	Role string // dev | pm | architect | ux | analyst | writer | user
	At   string // RFC3339 timestamp
}

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
	Turns        []Turn // deliberation transcript — not in default context bundle
	CreatedAt    string
	UpdatedAt    string
}

// PayloadSizeWarnThreshold is the approximate byte count at which AppendTurn
// emits a warning. Qdrant supports ~1 MB per payload; we warn at 80% headroom.
const PayloadSizeWarnThreshold = 800_000

// discTurnsLocks provides per-discussion in-process mutexes for AppendTurn.
// This is layered on top of the file-based flock so that both intra-process
// goroutines and cross-process agents are protected from lost-update races.
var discTurnsLocks sync.Map // key: discID (string) → *sync.Mutex

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
		"turns":         turnsToAny(d.Turns),
		"created_at":    d.CreatedAt,
		"updated_at":    d.UpdatedAt,
	})
	if err != nil {
		return "", fmt.Errorf("build payload: %w", err)
	}

	wait := true
	err = c.qdrantUpsert(ctx, &qdrant.UpsertPoints{
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

// OpenDiscussionsFilter returns the Qdrant filter that restricts results to
// open discussions only. Exported as a helper so tests can verify it directly.
func OpenDiscussionsFilter() *qdrant.Filter {
	return &qdrant.Filter{
		Must: []*qdrant.Condition{qdrant.NewMatchKeyword("status", "open")},
	}
}

// SearchDiscussions performs a semantic search across the discussions collection.
// When includeResolved is false (the default for agent consumption), only open
// discussions are returned — resolved and dismissed items are suppressed so
// agents don't surface stale context.
func (c *Client) SearchDiscussions(ctx context.Context, vector []float32, limit uint64, includeResolved bool) ([]Discussion, error) {
	req := &qdrant.QueryPoints{
		CollectionName: c.collDiscussions(),
		Query:          qdrant.NewQuery(vector...),
		Limit:          qdrant.PtrOf(limit),
		WithPayload:    qdrant.NewWithPayloadEnable(true),
	}
	if !includeResolved {
		req.Filter = OpenDiscussionsFilter()
	}
	results, err := c.qdrantQuery(ctx, req)
	if err != nil {
		return nil, err
	}
	discussions := make([]Discussion, 0, len(results))
	for _, r := range results {
		discussions = append(discussions, discussionFromPayload(r.GetPayload()))
	}
	return discussions, nil
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
		Turns:        extractTurns(pay["turns"]),
		CreatedAt:    pay["created_at"].GetStringValue(),
		UpdatedAt:    pay["updated_at"].GetStringValue(),
	}
}

// turnsToAny serializes []Turn to []any for qdrant.TryValueMap encoding.
func turnsToAny(turns []Turn) []any {
	if len(turns) == 0 {
		return nil
	}
	out := make([]any, len(turns))
	for i, t := range turns {
		out[i] = map[string]any{
			"by":   t.By,
			"text": t.Text,
			"role": t.Role,
			"at":   t.At,
		}
	}
	return out
}

// extractTurns deserializes a Qdrant ListValue of struct values into []Turn.
func extractTurns(v *qdrant.Value) []Turn {
	if v == nil {
		return nil
	}
	list := v.GetListValue()
	if list == nil {
		return nil
	}
	turns := make([]Turn, 0, len(list.Values))
	for _, item := range list.Values {
		fields := item.GetStructValue().GetFields()
		if fields == nil {
			continue
		}
		turns = append(turns, Turn{
			By:   fields["by"].GetStringValue(),
			Text: fields["text"].GetStringValue(),
			Role: fields["role"].GetStringValue(),
			At:   fields["at"].GetStringValue(),
		})
	}
	return turns
}

// estimateTurnsBytes returns a rough byte count for the turns slice to support
// the payload size guard in AppendTurn.
func estimateTurnsBytes(turns []Turn) int {
	total := 0
	for _, t := range turns {
		total += len(t.By) + len(t.Text) + len(t.Role) + len(t.At) + 32 // key overhead
	}
	return total
}

// AppendTurn adds a deliberation turn to an existing discussion.
// It warns (via the returned bool) if the estimated turns payload exceeds
// PayloadSizeWarnThreshold bytes — callers should surface this to the user.
//
// Concurrent safety: the read-modify-write is guarded by two layers:
//  1. A per-discussion in-process sync.Mutex (discTurnsLocks) serializes
//     goroutines within the same process.
//  2. A file-based flock (.gg/.disc-<ID>-turns.lock) serializes concurrent
//     agent processes on the same machine.
//
// Without both guards, two agents writing simultaneously would each read the
// same snapshot and overwrite each other's turn — silently losing one.
func (c *Client) AppendTurn(ctx context.Context, discID string, turn Turn) (sizeWarning bool, err error) {
	// Layer 1: in-process mutex — serializes goroutines within the same process.
	mu, _ := discTurnsLocks.LoadOrStore(discID, &sync.Mutex{})
	mu.(*sync.Mutex).Lock()
	defer mu.(*sync.Mutex).Unlock()

	// Layer 2: file flock — serializes across separate agent processes.
	// Only available when the client has a dataDir (a real project context).
	if c.dataDir != "" {
		lockPath := filepath.Join(c.dataDir, ".disc-"+discID+"-turns.lock")
		f, ferr := os.OpenFile(lockPath, os.O_RDWR|os.O_CREATE, 0600)
		if ferr != nil {
			return false, fmt.Errorf("open turns lock for %s: %w", discID, ferr)
		}
		defer func() { _ = f.Close() }()
		if lerr := lockFileCtx(ctx, f); lerr != nil {
			return false, fmt.Errorf("acquire turns lock for %s: %w", discID, lerr)
		}
		defer func() { _ = unlockFile(f) }()
	}

	pointID := qdrant.NewID(pointUUIDForDiscID(discID))

	// Fetch current turns to append to them.
	existing, err := c.qc.Get(ctx, &qdrant.GetPoints{
		CollectionName: c.collDiscussions(),
		Ids:            []*qdrant.PointId{pointID},
		WithPayload:    qdrant.NewWithPayloadEnable(true),
	})
	if err != nil {
		return false, err
	}
	if len(existing) == 0 {
		return false, fmt.Errorf("discussion %s not found", discID)
	}

	currentTurns := extractTurns(existing[0].GetPayload()["turns"])
	if turn.At == "" {
		turn.At = time.Now().UTC().Format(time.RFC3339)
	}
	newTurns := append(append([]Turn(nil), currentTurns...), turn)

	turnsVal, err := qdrant.NewValue(turnsToAny(newTurns))
	if err != nil {
		return false, fmt.Errorf("encode turns: %w", err)
	}
	updatedVal, _ := qdrant.NewValue(time.Now().UTC().Format(time.RFC3339))

	wait := true
	_, err = c.qc.SetPayload(ctx, &qdrant.SetPayloadPoints{
		CollectionName: c.collDiscussions(),
		Wait:           &wait,
		Payload: map[string]*qdrant.Value{
			"turns":      turnsVal,
			"updated_at": updatedVal,
		},
		PointsSelector: qdrant.NewPointsSelector(pointID),
	})
	if err != nil {
		return false, err
	}

	return estimateTurnsBytes(newTurns) > PayloadSizeWarnThreshold, nil
}

// ParseDiscID extracts the numeric suffix from a discussion ID like "DISC-001".
func ParseDiscID(id string) (int, error) {
	if !discIDRegex.MatchString(id) {
		return 0, fmt.Errorf("invalid discussion ID %q (expected DISC-NNN)", id)
	}
	return strconv.Atoi(id[5:])
}
