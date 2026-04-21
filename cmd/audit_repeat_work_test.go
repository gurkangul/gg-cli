package cmd

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/store"
)

// testConfig builds a RepeatWorkConfig with all thresholds at defaults so
// individual tests can focus on their signal without fixture-rewriting.
func testConfig() repeatWorkConfig {
	return repeatWorkConfig{
		WindowDays:        7,
		RecordsThreshold:  5,
		RecordsWindowDays: 3,
		ReopensThreshold:  2,
		FilesThreshold:    3,
		TagThreshold:      5,
	}
}

// rfc3339 renders a time `offset` ago as an RFC3339 string — used to build
// decision/bug timestamps inside and outside the windows under test.
func rfc3339(offset time.Duration) string {
	return time.Now().UTC().Add(-offset).Format(time.RFC3339)
}

// ── collectTierA: task records ──────────────────────────────────────────────

func TestCollectTierA_TaskRecords_BelowThresholdIgnored(t *testing.T) {
	cfg := testConfig()
	inner := time.Now().UTC().Add(-3 * 24 * time.Hour)
	var decs []store.Decision
	for i := 0; i < cfg.RecordsThreshold-1; i++ {
		decs = append(decs, store.Decision{
			Text: "x", TaskID: "TASK-100", CreatedAt: rfc3339(time.Duration(i) * time.Hour),
		})
	}
	got := collectTierA(decs, nil, inner, cfg)
	if len(got) != 0 {
		t.Errorf("below threshold must not trigger tier-A, got %v", got)
	}
}

func TestCollectTierA_TaskRecords_AtThresholdTriggers(t *testing.T) {
	cfg := testConfig()
	inner := time.Now().UTC().Add(-3 * 24 * time.Hour)
	var decs []store.Decision
	for i := 0; i < cfg.RecordsThreshold; i++ {
		decs = append(decs, store.Decision{
			Text: "x", TaskID: "TASK-100", CreatedAt: rfc3339(time.Duration(i) * time.Hour),
		})
	}
	got := collectTierA(decs, nil, inner, cfg)
	if len(got) != 1 {
		t.Fatalf("at threshold must trigger exactly one hotspot, got %d: %v", len(got), got)
	}
	if got[0].Key != "TASK-100" || got[0].Tier != "A" || got[0].Kind != "task" || got[0].Count != cfg.RecordsThreshold {
		t.Errorf("hotspot wrong: %+v", got[0])
	}
}

func TestCollectTierA_TaskRecords_OutsideWindowIgnored(t *testing.T) {
	cfg := testConfig()
	inner := time.Now().UTC().Add(-3 * 24 * time.Hour)
	// Seven records but all outside the 3-day inner window.
	var decs []store.Decision
	for i := 0; i < 7; i++ {
		decs = append(decs, store.Decision{
			Text: "x", TaskID: "TASK-100", CreatedAt: rfc3339(10 * 24 * time.Hour),
		})
	}
	got := collectTierA(decs, nil, inner, cfg)
	if len(got) != 0 {
		t.Errorf("records outside window must not trigger, got %v", got)
	}
}

// ── collectTierA: bug reopens ──────────────────────────────────────────────

func TestCollectTierA_BugReopens_BelowThresholdIgnored(t *testing.T) {
	cfg := testConfig()
	inner := time.Now().UTC().Add(-3 * 24 * time.Hour)
	bugs := []store.Bug{{ID: "BUG-001", ReopenCount: 1}}
	got := collectTierA(nil, bugs, inner, cfg)
	if len(got) != 0 {
		t.Errorf("1 reopen must not trigger when threshold=2, got %v", got)
	}
}

func TestCollectTierA_BugReopens_AtThresholdTriggers(t *testing.T) {
	cfg := testConfig()
	inner := time.Now().UTC().Add(-3 * 24 * time.Hour)
	bugs := []store.Bug{{ID: "BUG-001", ReopenCount: 2}}
	got := collectTierA(nil, bugs, inner, cfg)
	if len(got) != 1 {
		t.Fatalf("2 reopens must trigger, got %v", got)
	}
	if got[0].Key != "BUG-001" || got[0].Kind != "bug" || got[0].Count != 2 {
		t.Errorf("reopen hotspot wrong: %+v", got[0])
	}
}

// ── collectTierB: files touched by many bugs ───────────────────────────────

func TestCollectTierB_FileInThreeBugs_Triggers(t *testing.T) {
	cfg := testConfig()
	cutoff := time.Now().UTC().Add(-7 * 24 * time.Hour)
	ts := rfc3339(1 * time.Hour)
	bugs := []store.Bug{
		{ID: "BUG-1", AffectedFiles: []string{"cmd/foo.go"}, CreatedAt: ts, UpdatedAt: ts},
		{ID: "BUG-2", AffectedFiles: []string{"cmd/foo.go"}, CreatedAt: ts, UpdatedAt: ts},
		{ID: "BUG-3", AffectedFiles: []string{"cmd/foo.go"}, CreatedAt: ts, UpdatedAt: ts},
	}
	got := collectTierB(bugs, cutoff, cfg)
	if len(got) != 1 {
		t.Fatalf("3 bugs touching same file must trigger, got %v", got)
	}
	if got[0].Key != "cmd/foo.go" || got[0].Count != 3 {
		t.Errorf("file hotspot wrong: %+v", got[0])
	}
}

func TestCollectTierB_FileInOneBugIgnored(t *testing.T) {
	cfg := testConfig()
	cutoff := time.Now().UTC().Add(-7 * 24 * time.Hour)
	ts := rfc3339(1 * time.Hour)
	bugs := []store.Bug{
		{ID: "BUG-1", AffectedFiles: []string{"cmd/foo.go"}, CreatedAt: ts, UpdatedAt: ts},
	}
	got := collectTierB(bugs, cutoff, cfg)
	if len(got) != 0 {
		t.Errorf("1 bug must not trigger, got %v", got)
	}
}

// Deduplicated bug IDs — if the same bug is in the list twice somehow
// (shouldn't happen but defensive), file-count must still be 1 not 2.
func TestCollectTierB_FileDedup(t *testing.T) {
	cfg := testConfig()
	cfg.FilesThreshold = 2
	cutoff := time.Now().UTC().Add(-7 * 24 * time.Hour)
	ts := rfc3339(1 * time.Hour)
	bugs := []store.Bug{
		{ID: "BUG-1", AffectedFiles: []string{"a.go"}, CreatedAt: ts, UpdatedAt: ts},
		{ID: "BUG-1", AffectedFiles: []string{"a.go"}, CreatedAt: ts, UpdatedAt: ts}, // duplicate
	}
	got := collectTierB(bugs, cutoff, cfg)
	if len(got) != 0 {
		t.Errorf("duplicate bug IDs must dedup, got %v", got)
	}
}

// ── collectTierC: tag clusters ─────────────────────────────────────────────

func TestCollectTierC_TagAtThreshold_Triggers(t *testing.T) {
	cfg := testConfig()
	cutoff := time.Now().UTC().Add(-7 * 24 * time.Hour)
	var decs []store.Decision
	for i := 0; i < cfg.TagThreshold; i++ {
		decs = append(decs, store.Decision{
			Text: "x", Tags: []string{"race-condition"}, CreatedAt: rfc3339(time.Duration(i) * time.Hour),
		})
	}
	got := collectTierC(decs, cutoff, cfg)
	if len(got) != 1 {
		t.Fatalf("threshold tag-count must trigger, got %v", got)
	}
	if got[0].Key != "race-condition" || got[0].Count != cfg.TagThreshold {
		t.Errorf("tag hotspot wrong: %+v", got[0])
	}
}

func TestCollectTierC_TrivialTagsIgnored(t *testing.T) {
	cfg := testConfig()
	cutoff := time.Now().UTC().Add(-7 * 24 * time.Hour)
	// Five decisions all tagged 'impl' — trivial, must be skipped.
	var decs []store.Decision
	for i := 0; i < cfg.TagThreshold+2; i++ {
		decs = append(decs, store.Decision{
			Text: "x", Tags: []string{"impl"}, CreatedAt: rfc3339(time.Duration(i) * time.Hour),
		})
	}
	got := collectTierC(decs, cutoff, cfg)
	if len(got) != 0 {
		t.Errorf("trivial tag 'impl' must be skipped, got %v", got)
	}
}

// ── resolveRepeatWorkConfig ────────────────────────────────────────────────

func TestResolveRepeatWorkConfig_NilFallsBackToDefaults(t *testing.T) {
	c := resolveRepeatWorkConfig(nil)
	if c.WindowDays != repeatWorkDefaultWindowDays {
		t.Errorf("nil cfg must yield defaults, got WindowDays=%d", c.WindowDays)
	}
	if c.RecordsThreshold != repeatWorkDefaultRecordsThreshold {
		t.Errorf("RecordsThreshold fallback wrong: %d", c.RecordsThreshold)
	}
}

func TestResolveRepeatWorkConfig_OverridesApplied(t *testing.T) {
	cfg := &config.Config{}
	cfg.Audit.RepeatWork.WindowDays = 14
	cfg.Audit.RepeatWork.RecordsThreshold = 3
	c := resolveRepeatWorkConfig(cfg)
	if c.WindowDays != 14 {
		t.Errorf("WindowDays override lost: %d", c.WindowDays)
	}
	if c.RecordsThreshold != 3 {
		t.Errorf("RecordsThreshold override lost: %d", c.RecordsThreshold)
	}
	// Unset fields keep defaults.
	if c.TagThreshold != repeatWorkDefaultTagThreshold {
		t.Errorf("unset TagThreshold must default, got %d", c.TagThreshold)
	}
}

// ── renderRepeatWorkDefault: empty + populated ─────────────────────────────

func TestRenderRepeatWorkDefault_Empty(t *testing.T) {
	var buf bytes.Buffer
	renderRepeatWorkDefault(&buf, repeatWorkReport{}, testConfig())
	out := buf.String()
	if !strings.Contains(out, "No repeat-work hotspots") {
		t.Errorf("empty report must say so, got: %s", out)
	}
}

func TestRenderRepeatWorkDefault_HintVisibleWhenPopulated(t *testing.T) {
	var buf bytes.Buffer
	r := repeatWorkReport{TierA: []repeatWorkHotspot{
		{Tier: "A", Kind: "task", Key: "TASK-187", Count: 9, Detail: "9 records, spans 3.0d"},
	}}
	renderRepeatWorkDefault(&buf, r, testConfig())
	out := buf.String()
	if !strings.Contains(out, "TASK-187") {
		t.Errorf("hotspot key must appear: %s", out)
	}
	if !strings.Contains(out, "Hint:") {
		t.Errorf("advisory hint must be rendered: %s", out)
	}
}

func TestRenderRepeatWorkCompact_OneLinePerHotspot(t *testing.T) {
	var buf bytes.Buffer
	r := repeatWorkReport{
		TierA: []repeatWorkHotspot{{Tier: "A", Kind: "task", Key: "TASK-001", Count: 7, Detail: "d"}},
		TierB: []repeatWorkHotspot{{Tier: "B", Kind: "file", Key: "a.go", Count: 3, Detail: "d"}},
	}
	renderRepeatWorkCompact(&buf, r)
	out := buf.String()
	if !strings.Contains(out, "repeat-work — 1A 1B 0C") {
		t.Errorf("header missing: %s", out)
	}
	if !strings.Contains(out, "A  task  TASK-001  7") {
		t.Errorf("tier-A line wrong: %s", out)
	}
	if !strings.Contains(out, "B  file  a.go  3") {
		t.Errorf("tier-B line wrong: %s", out)
	}
}

// ── humaniseSpan ───────────────────────────────────────────────────────────

func TestHumaniseSpan(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want string
	}{
		{0, "<1h"},
		{30 * time.Minute, "<1h"},
		{90 * time.Minute, "1h"},
		{23 * time.Hour, "23h"},
		{25 * time.Hour, "1.0d"},
		{72 * time.Hour, "3.0d"},
		{80 * time.Hour, "3.3d"},
	}
	for _, tc := range cases {
		got := humaniseSpan(tc.in)
		if got != tc.want {
			t.Errorf("humaniseSpan(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
