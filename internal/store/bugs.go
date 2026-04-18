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

var bugIDNamespace = uuid.MustParse("c0c0c0c0-1a5c-4d0d-bab0-000000000003")
var bugIDRegex = regexp.MustCompile(`^BUG-\d{3,}$`)

// Bug represents a structured defect report with lifecycle tracking.
// Lifecycle: open → fixing → fixed | wontfix
type Bug struct {
	ID              string
	Title           string
	Detail          string
	Severity        string // critical | high | medium | low
	Status          string // open | fixing | fixed | wontfix
	RootCause       string // filled on fix
	FixSummary      string // short description of the fix
	TaskID          string // optional linked task
	Tags            []string
	AffectedFiles   []string // source file paths this bug touches (mirrors Memgraph AFFECTS edges)
	AffectedSymbols []string // symbol names this bug touches (mirrors Memgraph AFFECTS edges)
	CreatedAt       string
	UpdatedAt       string
}

func pointUUIDForBugID(id string) string {
	return uuid.NewSHA1(bugIDNamespace, []byte(id)).String()
}

func (c *Client) maxBugIDNumber(ctx context.Context) (int, error) {
	points, err := c.scrollAll(ctx, &qdrant.ScrollPoints{
		CollectionName: c.collBugs(),
		Limit:          qdrant.PtrOf(uint32(1000)),
		WithPayload:    qdrant.NewWithPayloadInclude("bug_id"),
	})
	if err != nil {
		return 0, fmt.Errorf("scan bugs collection: %w", err)
	}
	maxNum := 0
	for _, p := range points {
		id := p.GetPayload()["bug_id"].GetStringValue()
		if n, err := ParseBugID(id); err == nil && n > maxNum {
			maxNum = n
		}
	}
	return maxNum, nil
}

func (c *Client) ReportBug(ctx context.Context, b Bug, vector []float32) (string, error) {
	id, err := c.allocBugID(ctx)
	if err != nil {
		return "", err
	}
	b.ID = id
	if b.Status == "" {
		b.Status = "open"
	}
	if b.Severity == "" {
		b.Severity = "medium"
	}
	now := time.Now().UTC().Format(time.RFC3339)
	b.CreatedAt = now
	b.UpdatedAt = now

	payload, err := qdrant.TryValueMap(map[string]any{
		"bug_id":           b.ID,
		"title":            b.Title,
		"detail":           b.Detail,
		"severity":         b.Severity,
		"status":           b.Status,
		"root_cause":       b.RootCause,
		"fix_summary":      b.FixSummary,
		"task_id":          b.TaskID,
		"tags":             toAnySlice(b.Tags),
		"affected_files":   toAnySlice(b.AffectedFiles),
		"affected_symbols": toAnySlice(b.AffectedSymbols),
		"created_at":       b.CreatedAt,
		"updated_at":       b.UpdatedAt,
	})
	if err != nil {
		return "", fmt.Errorf("build payload: %w", err)
	}

	wait := true
	err = c.qdrantUpsert(ctx, &qdrant.UpsertPoints{
		CollectionName: c.collBugs(),
		Wait:           &wait,
		Points: []*qdrant.PointStruct{
			{
				Id:      qdrant.NewID(pointUUIDForBugID(b.ID)),
				Vectors: qdrant.NewVectors(vector...),
				Payload: payload,
			},
		},
	})
	if err != nil {
		return "", err
	}
	return b.ID, nil
}

func (c *Client) GetBug(ctx context.Context, bugID string) (*Bug, error) {
	points, err := c.qc.Get(ctx, &qdrant.GetPoints{
		CollectionName: c.collBugs(),
		Ids:            []*qdrant.PointId{qdrant.NewID(pointUUIDForBugID(bugID))},
		WithPayload:    qdrant.NewWithPayloadEnable(true),
	})
	if err != nil {
		return nil, err
	}
	if len(points) == 0 {
		return nil, fmt.Errorf("bug %s not found", bugID)
	}
	b := bugFromPayload(points[0].GetPayload())
	return &b, nil
}

func (c *Client) ListBugs(ctx context.Context, statusFilter string) ([]Bug, error) {
	req := &qdrant.ScrollPoints{
		CollectionName: c.collBugs(),
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
	bugs := make([]Bug, 0, len(points))
	for _, p := range points {
		bugs = append(bugs, bugFromPayload(p.GetPayload()))
	}
	sort.Slice(bugs, func(i, j int) bool {
		ni, _ := ParseBugID(bugs[i].ID)
		nj, _ := ParseBugID(bugs[j].ID)
		return ni < nj
	})
	return bugs, nil
}

// FixBug transitions a bug to "fixed" and records the root cause and fix summary.
func (c *Client) FixBug(ctx context.Context, bugID, rootCause, fixSummary string) error {
	return c.updateBugStatus(ctx, bugID, "fixed", rootCause, fixSummary)
}

// WontFixBug transitions a bug to "wontfix" with a reason in fixSummary.
func (c *Client) WontFixBug(ctx context.Context, bugID, reason string) error {
	return c.updateBugStatus(ctx, bugID, "wontfix", "", reason)
}

// StartFixingBug transitions a bug to "fixing".
func (c *Client) StartFixingBug(ctx context.Context, bugID string) error {
	return c.updateBugStatus(ctx, bugID, "fixing", "", "")
}

func (c *Client) updateBugStatus(ctx context.Context, bugID, status, rootCause, fixSummary string) error {
	pointID := qdrant.NewID(pointUUIDForBugID(bugID))
	existing, err := c.qc.Get(ctx, &qdrant.GetPoints{
		CollectionName: c.collBugs(),
		Ids:            []*qdrant.PointId{pointID},
		WithPayload:    qdrant.NewWithPayloadInclude("bug_id", "status"),
	})
	if err != nil {
		return err
	}
	if len(existing) == 0 {
		return fmt.Errorf("bug %s not found", bugID)
	}
	currentStatus := existing[0].GetPayload()["status"].GetStringValue()
	if currentStatus == status {
		return fmt.Errorf("%w: bug %s already %s — refusing to overwrite root_cause/summary (concurrent update?)", ErrAlreadyInState, bugID, status)
	}

	statusVal, _ := qdrant.NewValue(status)
	rootVal, _ := qdrant.NewValue(rootCause)
	fixVal, _ := qdrant.NewValue(fixSummary)
	updVal, _ := qdrant.NewValue(time.Now().UTC().Format(time.RFC3339))

	payload := map[string]*qdrant.Value{
		"status":      statusVal,
		"root_cause":  rootVal,
		"fix_summary": fixVal,
		"updated_at":  updVal,
	}

	wait := true
	_, err = c.qc.SetPayload(ctx, &qdrant.SetPayloadPoints{
		CollectionName: c.collBugs(),
		Wait:           &wait,
		Payload:        payload,
		PointsSelector: qdrant.NewPointsSelector(pointID),
	})
	return err
}

func (c *Client) SearchBugs(ctx context.Context, vector []float32, limit uint64) ([]Bug, error) {
	results, err := c.qdrantQuery(ctx, &qdrant.QueryPoints{
		CollectionName: c.collBugs(),
		Query:          qdrant.NewQuery(vector...),
		Limit:          qdrant.PtrOf(limit),
		WithPayload:    qdrant.NewWithPayloadEnable(true),
	})
	if err != nil {
		return nil, err
	}
	bugs := make([]Bug, 0, len(results))
	for _, r := range results {
		bugs = append(bugs, bugFromPayload(r.GetPayload()))
	}
	return bugs, nil
}

func bugFromPayload(pay map[string]*qdrant.Value) Bug {
	return Bug{
		ID:              pay["bug_id"].GetStringValue(),
		Title:           pay["title"].GetStringValue(),
		Detail:          pay["detail"].GetStringValue(),
		Severity:        pay["severity"].GetStringValue(),
		Status:          pay["status"].GetStringValue(),
		RootCause:       pay["root_cause"].GetStringValue(),
		FixSummary:      pay["fix_summary"].GetStringValue(),
		TaskID:          pay["task_id"].GetStringValue(),
		Tags:            extractStringList(pay["tags"]),
		AffectedFiles:   extractStringList(pay["affected_files"]),
		AffectedSymbols: extractStringList(pay["affected_symbols"]),
		CreatedAt:       pay["created_at"].GetStringValue(),
		UpdatedAt:       pay["updated_at"].GetStringValue(),
	}
}

// ParseBugID extracts the numeric suffix from a bug ID like "BUG-001".
func ParseBugID(id string) (int, error) {
	if !bugIDRegex.MatchString(id) {
		return 0, fmt.Errorf("invalid bug ID %q (expected BUG-NNN)", id)
	}
	return strconv.Atoi(id[4:])
}
