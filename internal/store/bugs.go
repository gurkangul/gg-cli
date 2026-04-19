package store

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/qdrant/go-client/qdrant"
)

var bugIDNamespace = uuid.MustParse("c0c0c0c0-1a5c-4d0d-bab0-000000000003")
var bugIDRegex = regexp.MustCompile(`^BUG-\d{3,}$`)

// Bug represents a structured defect report with lifecycle tracking.
// Lifecycle: open → fixing → fixed | wontfix → reopened → fixing → fixed
type Bug struct {
	ID              string
	Title           string
	Detail          string
	Severity        string // critical | high | medium | low
	Status          string // open | fixing | fixed | wontfix | reopened
	RootCause       string // filled on fix
	FixSummary      string // short description of the fix
	ReproScript     string // path to repro script or *_test.go file; "REPRO-MISSING" for legacy bugs
	TaskID          string // optional linked task
	Tags            []string
	AffectedFiles   []string // source file paths this bug touches (mirrors Memgraph AFFECTS edges)
	AffectedSymbols []string // symbol names this bug touches (mirrors Memgraph AFFECTS edges)
	ReopenCount     int      // number of times this bug was reopened after being fixed
	ReopenReasons   []string // reason provided at each reopen, in order
	By              string   // agent role or user that reported this bug
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
		"repro_script":     b.ReproScript,
		"task_id":          b.TaskID,
		"tags":             toAnySlice(b.Tags),
		"affected_files":   toAnySlice(b.AffectedFiles),
		"affected_symbols": toAnySlice(b.AffectedSymbols),
		"reopen_count":     float64(0),
		"reopen_reasons":   toAnySlice(nil),
		"by":               b.By,
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

// CountBugs returns the number of bugs with the given status; pass an empty
// string to count all. Mirrors CountTasks so status renderers can pull a
// cheap summary without scrolling every payload.
func (c *Client) CountBugs(ctx context.Context, statusFilter string) (uint64, error) {
	req := &qdrant.CountPoints{
		CollectionName: c.collBugs(),
	}
	if statusFilter != "" {
		req.Filter = &qdrant.Filter{
			Must: []*qdrant.Condition{qdrant.NewMatchKeyword("status", statusFilter)},
		}
	}
	return c.qc.Count(ctx, req)
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

// FixBug transitions a bug to "fixed" and records the root cause, fix summary,
// and the repro script path that guards against regression.
func (c *Client) FixBug(ctx context.Context, bugID, rootCause, fixSummary, reproScript string) error {
	return c.updateBugStatus(ctx, bugID, "fixed", rootCause, fixSummary, reproScript)
}

// WontFixBug transitions a bug to "wontfix" with a reason and an optional repro
// script that documents the confirmed-but-accepted failure mode.
func (c *Client) WontFixBug(ctx context.Context, bugID, reason, reproScript string) error {
	return c.updateBugStatus(ctx, bugID, "wontfix", "", reason, reproScript)
}

// StartFixingBug transitions a bug to "fixing".
func (c *Client) StartFixingBug(ctx context.Context, bugID string) error {
	return c.updateBugStatus(ctx, bugID, "fixing", "", "", "")
}

func (c *Client) updateBugStatus(ctx context.Context, bugID, status, rootCause, fixSummary, reproScript string) error {
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
	reproVal, _ := qdrant.NewValue(reproScript)
	updVal, _ := qdrant.NewValue(time.Now().UTC().Format(time.RFC3339))

	payload := map[string]*qdrant.Value{
		"status":       statusVal,
		"root_cause":   rootVal,
		"fix_summary":  fixVal,
		"repro_script": reproVal,
		"updated_at":   updVal,
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

// ReopenBug transitions a fixed or wontfix bug back to "reopened" and records
// the reason. The reopen_count is incremented and the reason is appended to
// reopen_reasons so the full history is preserved.
func (c *Client) ReopenBug(ctx context.Context, bugID, reason string) error {
	pointID := qdrant.NewID(pointUUIDForBugID(bugID))
	existing, err := c.qc.Get(ctx, &qdrant.GetPoints{
		CollectionName: c.collBugs(),
		Ids:            []*qdrant.PointId{pointID},
		WithPayload:    qdrant.NewWithPayloadEnable(true),
	})
	if err != nil {
		return err
	}
	if len(existing) == 0 {
		return fmt.Errorf("bug %s not found", bugID)
	}
	pay := existing[0].GetPayload()
	currentStatus := pay["status"].GetStringValue()
	if currentStatus != "fixed" && currentStatus != "wontfix" {
		return fmt.Errorf("bug %s is %s — can only reopen a fixed or wontfix bug", bugID, currentStatus)
	}

	newCount := int(pay["reopen_count"].GetDoubleValue()) + 1
	existingReasons := extractStringList(pay["reopen_reasons"])
	existingReasons = append(existingReasons, reason)

	statusVal, _ := qdrant.NewValue("reopened")
	countVal, _ := qdrant.NewValue(float64(newCount))
	reasonsVal, _ := qdrant.NewValue(toAnySlice(existingReasons))
	updVal, _ := qdrant.NewValue(time.Now().UTC().Format(time.RFC3339))

	wait := true
	_, err = c.qc.SetPayload(ctx, &qdrant.SetPayloadPoints{
		CollectionName: c.collBugs(),
		Wait:           &wait,
		Payload: map[string]*qdrant.Value{
			"status":         statusVal,
			"reopen_count":   countVal,
			"reopen_reasons": reasonsVal,
			"updated_at":     updVal,
		},
		PointsSelector: qdrant.NewPointsSelector(pointID),
	})
	return err
}

// BugHealthStats summarises the stability trend for a lookback window, giving
// the rough reopen-rate signal gg audit surfaces as a quality thermometer.
//
// reopen_rate = Reopens / (Reopens + FreshCloses). High rates (>20%) mean the
// team is shipping "done" that later comes back — the signature of the
// premature-closure pattern surfaced by dogfood audit 2026-04-19.
type BugHealthStats struct {
	Reopens     int // total reopen transitions logged in the window
	FreshCloses int // bugs closed (fixed|wontfix) in the window with no reopen history
}

// BugHealthStatsSince returns reopen and fresh-close counts for bugs whose
// updated_at falls within the last sinceDays. Values are independent counts
// (Reopens increments on reopen; FreshCloses increments on initial close) so
// the caller computes the ratio and applies thresholds.
//
// sinceDays <= 0 is treated as 7.
func (c *Client) BugHealthStatsSince(ctx context.Context, sinceDays int) (BugHealthStats, error) {
	if sinceDays <= 0 {
		sinceDays = 7
	}
	points, err := c.scrollAll(ctx, &qdrant.ScrollPoints{
		CollectionName: c.collBugs(),
		WithPayload:    qdrant.NewWithPayloadEnable(true),
	})
	if err != nil {
		return BugHealthStats{}, err
	}
	cutoff := time.Now().UTC().Add(-time.Duration(sinceDays) * 24 * time.Hour)
	var stats BugHealthStats
	for _, p := range points {
		pay := p.GetPayload()
		updatedAt := pay["updated_at"].GetStringValue()
		t, parseErr := time.Parse(time.RFC3339, updatedAt)
		if parseErr != nil || t.Before(cutoff) {
			continue
		}
		status := pay["status"].GetStringValue()
		rc := int(pay["reopen_count"].GetDoubleValue())
		if rc > 0 {
			stats.Reopens += rc
		}
		// A "fresh close" is a bug that reached fixed/wontfix without having
		// been reopened. Reopened-then-fixed bugs are counted as reopens only
		// so the ratio captures the cost of getting it right the second time.
		if rc == 0 && (status == "fixed" || status == "wontfix") {
			stats.FreshCloses++
		}
	}
	return stats, nil
}

// BugReopenStats returns the total reopen count across all bugs updated within
// the past 7 days, plus a breakdown by top-level directory from affected_files.
func (c *Client) BugReopenStats(ctx context.Context) (total int, byDir map[string]int, err error) {
	points, err := c.scrollAll(ctx, &qdrant.ScrollPoints{
		CollectionName: c.collBugs(),
		WithPayload:    qdrant.NewWithPayloadEnable(true),
	})
	if err != nil {
		return 0, nil, err
	}

	cutoff := time.Now().UTC().Add(-7 * 24 * time.Hour)
	byDir = make(map[string]int)

	for _, p := range points {
		pay := p.GetPayload()
		rc := int(pay["reopen_count"].GetDoubleValue())
		if rc == 0 {
			continue
		}
		updatedAt := pay["updated_at"].GetStringValue()
		t, parseErr := time.Parse(time.RFC3339, updatedAt)
		if parseErr != nil || t.Before(cutoff) {
			continue
		}
		total += rc
		for _, f := range extractStringList(pay["affected_files"]) {
			dir := topLevelDir(f)
			if dir != "" {
				byDir[dir] += rc
			}
		}
	}
	return total, byDir, nil
}

// topLevelDir returns the first path component of a slash-separated path.
// Returns empty string for bare filenames with no directory component.
func topLevelDir(path string) string {
	if idx := strings.Index(path, "/"); idx > 0 {
		return path[:idx]
	}
	return ""
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
		ReproScript:     pay["repro_script"].GetStringValue(),
		TaskID:          pay["task_id"].GetStringValue(),
		Tags:            extractStringList(pay["tags"]),
		AffectedFiles:   extractStringList(pay["affected_files"]),
		AffectedSymbols: extractStringList(pay["affected_symbols"]),
		ReopenCount:     int(pay["reopen_count"].GetDoubleValue()),
		ReopenReasons:   extractStringList(pay["reopen_reasons"]),
		By:              pay["by"].GetStringValue(),
		CreatedAt:       pay["created_at"].GetStringValue(),
		UpdatedAt:       pay["updated_at"].GetStringValue(),
	}
}

// SetBugReproScript attaches or replaces the repro_script field on an existing
// bug without changing its status. Used to backfill repros on already-fixed bugs.
func (c *Client) SetBugReproScript(ctx context.Context, bugID, reproScript string) error {
	pointID := qdrant.NewID(pointUUIDForBugID(bugID))
	existing, err := c.qc.Get(ctx, &qdrant.GetPoints{
		CollectionName: c.collBugs(),
		Ids:            []*qdrant.PointId{pointID},
		WithPayload:    qdrant.NewWithPayloadInclude("bug_id"),
	})
	if err != nil {
		return err
	}
	if len(existing) == 0 {
		return fmt.Errorf("bug %s not found", bugID)
	}
	reproVal, _ := qdrant.NewValue(reproScript)
	updVal, _ := qdrant.NewValue(time.Now().UTC().Format(time.RFC3339))
	wait := true
	_, err = c.qc.SetPayload(ctx, &qdrant.SetPayloadPoints{
		CollectionName: c.collBugs(),
		Wait:           &wait,
		Payload: map[string]*qdrant.Value{
			"repro_script": reproVal,
			"updated_at":   updVal,
		},
		PointsSelector: qdrant.NewPointsSelector(pointID),
	})
	return err
}

// ParseBugID extracts the numeric suffix from a bug ID like "BUG-001".
func ParseBugID(id string) (int, error) {
	if !bugIDRegex.MatchString(id) {
		return 0, fmt.Errorf("invalid bug ID %q (expected BUG-NNN)", id)
	}
	return strconv.Atoi(id[4:])
}
