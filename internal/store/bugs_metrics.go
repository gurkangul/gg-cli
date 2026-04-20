package store

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/qdrant/go-client/qdrant"
)

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

// SurfacePressureStats summarises how many distinct files each bug fix touched,
// used by `gg audit health` to detect scattered-state patterns.
type SurfacePressureStats struct {
	P95FilesPerFix int // 95th-percentile of distinct files touched per fixed bug
	Median         int // median of the same distribution
	SampleSize     int // number of fixed bugs with at least one affected_file recorded
}

// SurfacePressureSince computes file-spread statistics for bugs whose
// updated_at falls within the last sinceDays and whose status is "fixed" or
// "wontfix". Bugs with no affected_files are excluded from the sample (they
// predate the --root-cause file convention). sinceDays <= 0 defaults to 7.
func (c *Client) SurfacePressureSince(ctx context.Context, sinceDays int) (SurfacePressureStats, error) {
	if sinceDays <= 0 {
		sinceDays = 7
	}
	points, err := c.scrollAll(ctx, &qdrant.ScrollPoints{
		CollectionName: c.collBugs(),
		WithPayload:    qdrant.NewWithPayloadEnable(true),
	})
	if err != nil {
		return SurfacePressureStats{}, err
	}
	cutoff := time.Now().UTC().Add(-time.Duration(sinceDays) * 24 * time.Hour)
	var counts []int
	for _, p := range points {
		pay := p.GetPayload()
		status := pay["status"].GetStringValue()
		if status != "fixed" && status != "wontfix" {
			continue
		}
		updatedAt := pay["updated_at"].GetStringValue()
		t, parseErr := time.Parse(time.RFC3339, updatedAt)
		if parseErr != nil || t.Before(cutoff) {
			continue
		}
		files := extractStringList(pay["affected_files"])
		if len(files) == 0 {
			continue
		}
		counts = append(counts, len(files))
	}
	if len(counts) == 0 {
		return SurfacePressureStats{}, nil
	}
	sort.Ints(counts)
	return SurfacePressureStats{
		P95FilesPerFix: percentile95(counts),
		Median:         counts[len(counts)/2],
		SampleSize:     len(counts),
	}, nil
}

// percentile95 returns the 95th-percentile value from a sorted integer slice
// using the nearest-rank method: rank = ceil(0.95 * n).
func percentile95(sorted []int) int {
	if len(sorted) == 0 {
		return 0
	}
	// Integer ceiling of 0.95*n without importing math.
	rank := (len(sorted)*95 + 99) / 100
	if rank > len(sorted) {
		rank = len(sorted)
	}
	return sorted[rank-1]
}

// topLevelDir returns the first path component of a slash-separated path.
// Returns empty string for bare filenames with no directory component.
func topLevelDir(path string) string {
	if idx := strings.Index(path, "/"); idx > 0 {
		return path[:idx]
	}
	return ""
}
