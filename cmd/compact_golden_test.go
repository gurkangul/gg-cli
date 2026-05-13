package cmd

// Golden file determinism tests for the compact renderer.
//
// Each sub-test renders a named compact function with a fixed, realistic
// input fixture and compares the output byte-for-byte against a .golden
// file in testdata/compact/.
//
// Run with -update to regenerate all golden files after an intentional
// format change. Bumping compactRendererV in compact.go is the signal
// that golden files need updating — the rendererV assertion below will
// fail and remind the author to run -update.
//
// Usage:
//   go test ./cmd/ -run TestCompactGolden              # verify
//   go test ./cmd/ -run TestCompactGolden -update      # regenerate

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gurkangul/gg-cli/internal/contextartifacts"
	"github.com/gurkangul/gg-cli/internal/graph"
	"github.com/gurkangul/gg-cli/internal/store"
)

var updateGolden = flag.Bool("update", false, "regenerate compact golden files")

// expectedRendererVersions pins each verb's compact renderer version.
//
// When a verb's output format changes, bump ONLY that verb's constant in
// compact.go AND update the corresponding value in this map — then run
// -update to regenerate that verb's golden file. Keeping the map here means
// the test fails loudly on any unintended version bump, and a reviewer can
// see exactly which verbs changed and confirm their golden files were updated.
var expectedRendererVersions = map[string]int{
	"search":     compactRendererV_search,
	"context":    compactRendererV_context,
	"impact":     compactRendererV_impact,
	"inbox":      compactRendererV_inbox,
	"taskList":   compactRendererV_taskList,
	"taskGet":    compactRendererV_taskGet,
	"bugList":    compactRendererV_bugList,
	"decideGaps": compactRendererV_decideGaps,
	"repeatWork": compactRendererV_repeatWork,
}

// expectedRendererV_each holds the known-good version for each verb.
// Update ONLY the verb you changed — leaving others untouched signals
// that their golden files are still valid.
var expectedRendererV_each = map[string]int{
	"search":     3,
	"context":    3,
	"impact":     2,
	"inbox":      2,
	"taskList":   2,
	"taskGet":    2,
	"bugList":    2,
	"decideGaps": 2,
	"repeatWork": 2,
}

func TestCompactRendererV_Pinned(t *testing.T) {
	for verb, got := range expectedRendererVersions {
		want, ok := expectedRendererV_each[verb]
		if !ok {
			t.Errorf("verb %q has a version constant but no expected entry in expectedRendererV_each — add it", verb)
			continue
		}
		if got != want {
			t.Errorf("compactRendererV_%s changed from %d to %d — "+
				"run `go test ./cmd/ -run TestCompactGolden -update` to regenerate golden files, "+
				"then update expectedRendererV_each[%q] to %d",
				verb, want, got, verb, got)
		}
	}
}

// ── Fixtures ─────────────────────────────────────────────────────────────────
//
// All timestamps are fixed ISO-8601 strings so golden output is
// deterministic regardless of when tests run.

var goldenDecisions = []store.Decision{
	{
		Text:      "use JWT for stateless auth",
		CreatedAt: "2026-04-10T12:00:00Z",
		TaskID:    "TASK-001",
		Reason:    "scales horizontally — must NOT appear in compact",
		Tags:      []string{"auth", "backend"},
	},
	{
		Text:      "rotate signing keys every 90 days",
		CreatedAt: "2026-04-08T09:30:00Z",
		TaskID:    "",
		Reason:    "SOC2 alignment — must NOT appear",
	},
}

var goldenRejections = []store.Rejection{
	{
		Approach:  "session cookies with Redis store",
		CreatedAt: "2026-04-09T11:00:00Z",
		Reason:    "stateful dependency in hot path — must NOT appear",
		TaskID:    "TASK-010",
	},
}

var goldenTasks = []store.Task{
	{ID: "TASK-042", Title: "implement JWT refresh endpoint", Status: "in_progress", Priority: "high", Detail: "must NOT appear"},
	{ID: "TASK-007", Title: "add JWT revocation cache", Status: "pending", Priority: "medium", Detail: "must NOT appear"},
	{ID: "TASK-003", Title: "write auth integration test", Status: "done", Priority: "low"},
}

var goldenBugs = []store.Bug{
	{ID: "BUG-007", Severity: "high", Status: "open", Title: "token refresh race condition"},
	{ID: "BUG-002", Severity: "low", Status: "fixed", Title: "typo in error message"},
}

var goldenNotes = []store.Note{
	{Text: "saw 15% refresh spike between 09:00-10:00 UTC", CreatedAt: "2026-04-08T10:15:00Z", TaskID: "TASK-042"},
	{Text: "deploy on Tuesday", CreatedAt: "2026-04-11T08:00:00Z"},
}

var goldenDiscussions = []store.Discussion{
	{
		ID: "DISC-008", Topic: "adopt FIDO2 alongside passwords?", Status: "open",
		Detail: "must NOT appear",
		Turns:  []store.Turn{{By: "sec-lead"}, {By: "platform"}, {By: "product"}},
	},
	{
		ID: "DISC-003", Topic: "API versioning strategy", Status: "resolved",
		ResolvedNote: "must NOT appear",
	},
}

var goldenMessages = []store.Message{
	{
		FromRole: "pm", ToRole: "developer",
		Content:   "ship TASK-042 by Friday",
		CreatedAt: "2026-04-20T10:00:00Z",
		TaskID:    "TASK-042",
	},
	{
		FromRole: "qa", ToRole: "developer",
		Content:   "line1\nline2 multiline folded",
		CreatedAt: "2026-04-20T09:30:00Z",
	},
}

var goldenArtifacts = []contextartifacts.Snippet{
	{
		Path:      "docs/auth.md",
		StartLine: 12,
		EndLine:   14,
		Stale:     true,
		Text:      "refresh tokens rotate on every use\nJWT signing keys live in KMS",
	},
}

// ── Golden helpers ────────────────────────────────────────────────────────────

func goldenPath(name string) string {
	return filepath.Join("testdata", "compact", name+".golden")
}

// checkOrUpdate reads the named golden file and compares got to its contents.
// When -update is set it writes got as the new golden.
func checkOrUpdate(t *testing.T, name, got string) {
	t.Helper()
	path := goldenPath(name)

	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden %s: %v", path, err)
		}
		t.Logf("updated %s", path)
		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("golden file %s missing — run with -update to create it: %v", path, err)
	}
	if got != string(want) {
		t.Errorf("compact output for %q does not match golden file %s\n\n--- want ---\n%s\n--- got ---\n%s\n--- diff (line-by-line) ---\n%s",
			name, path, want, got, lineDiff(string(want), got))
	}
}

// lineDiff produces a simple line-by-line diff so failures are readable.
func lineDiff(want, got string) string {
	wLines := strings.Split(want, "\n")
	gLines := strings.Split(got, "\n")
	var sb strings.Builder
	max := len(wLines)
	if len(gLines) > max {
		max = len(gLines)
	}
	for i := range max {
		w := ""
		g := ""
		if i < len(wLines) {
			w = wLines[i]
		}
		if i < len(gLines) {
			g = gLines[i]
		}
		if w == g {
			sb.WriteString("  " + w + "\n")
		} else {
			sb.WriteString("- " + w + "\n")
			sb.WriteString("+ " + g + "\n")
		}
	}
	return sb.String()
}

// ── Line builder golden tests ─────────────────────────────────────────────────

func TestCompactGolden_LineBuilders(t *testing.T) {
	var buf bytes.Buffer

	for _, d := range goldenDecisions {
		buf.WriteString(compactDecisionLine(d) + "\n")
	}
	for _, r := range goldenRejections {
		buf.WriteString(compactRejectionLine(r) + "\n")
	}
	for _, tk := range goldenTasks {
		buf.WriteString(compactTaskLine(tk) + "\n")
	}
	for _, b := range goldenBugs {
		buf.WriteString(compactBugLine(b) + "\n")
	}
	for _, n := range goldenNotes {
		buf.WriteString(compactNoteLine(n) + "\n")
	}
	for _, d := range goldenDiscussions {
		buf.WriteString(compactDiscussionLine(d) + "\n")
	}
	for _, m := range goldenMessages {
		buf.WriteString(compactMessageLine(m) + "\n")
	}
	for _, a := range goldenArtifacts {
		buf.WriteString(compactArtifactLine(a) + "\n")
	}

	checkOrUpdate(t, "line_builders", buf.String())
}

// ── renderContextCompact golden test ─────────────────────────────────────────

func TestCompactGolden_ContextCompact(t *testing.T) {
	bundle := contextBundle{
		decisions:   goldenDecisions,
		rejections:  goldenRejections,
		tasks:       goldenTasks,
		discussions: goldenDiscussions,
		notes:       goldenNotes,
		artifacts:   goldenArtifacts,
	}
	var buf bytes.Buffer
	renderContextCompact(&buf, "auth", bundle, []string{"embedder timeout"}, nil)
	checkOrUpdate(t, "context_compact", buf.String())
}

// ── renderSearchCompact golden test ──────────────────────────────────────────

func TestCompactGolden_SearchCompact(t *testing.T) {
	var buf bytes.Buffer
	renderSearchCompact(&buf, goldenDecisions, goldenRejections)
	checkOrUpdate(t, "search_compact", buf.String())
}

func TestCompactGolden_SearchCompact_Empty(t *testing.T) {
	var buf bytes.Buffer
	renderSearchCompact(&buf, nil, nil)
	checkOrUpdate(t, "search_compact_empty", buf.String())
}

// ── renderImpactCompact (file) golden test ────────────────────────────────────

func TestCompactGolden_ImpactFile(t *testing.T) {
	r := impactResult{
		File:       "internal/store/store.go",
		Dependents: []string{"cmd/context.go", "cmd/search.go"},
		Symbols:    []map[string]any{{"name": "Decision"}, {"name": "Task"}},
		Decisions:  goldenDecisions[:1],
		Tasks:      goldenTasks[:2],
		Rejections: goldenRejections,
		HistoricalBugs: []graph.BugRef{
			{BugID: "BUG-007", Title: "token refresh race condition"},
		},
		Warnings: []string{"graph unavailable"},
	}
	var buf bytes.Buffer
	renderImpactCompact(&buf, r)
	checkOrUpdate(t, "impact_file", buf.String())
}

// ── renderTaskImpactCompact golden test ──────────────────────────────────────

func TestCompactGolden_ImpactTask(t *testing.T) {
	r := taskImpactResult{
		TaskID:     "TASK-042",
		Dependents: []string{"TASK-101", "TASK-102"},
		Siblings:   []string{"TASK-040", "TASK-041"},
		Decisions:  goldenDecisions[:1],
		Tasks:      goldenTasks[1:3],
		Bugs:       goldenBugs[:1],
		Rejections: goldenRejections,
	}
	var buf bytes.Buffer
	renderTaskImpactCompact(&buf, r)
	checkOrUpdate(t, "impact_task", buf.String())
}

// ── renderBugImpactCompact golden test ───────────────────────────────────────

func TestCompactGolden_ImpactBug(t *testing.T) {
	r := bugImpactResult{
		BugID:      "BUG-007",
		Files:      []string{"internal/store/store.go", "cmd/context.go"},
		Symbols:    []string{"FetchDecisions", "SearchDecisions"},
		Decisions:  goldenDecisions[:1],
		Tasks:      goldenTasks[:1],
		Rejections: goldenRejections,
		Warnings:   []string{"symbol index stale"},
	}
	var buf bytes.Buffer
	renderBugImpactCompact(&buf, r)
	checkOrUpdate(t, "impact_bug", buf.String())
}

// ── renderTaskListCompact golden test ────────────────────────────────────────

func TestCompactGolden_TaskList(t *testing.T) {
	var buf bytes.Buffer
	renderTaskListCompact(&buf, goldenTasks)
	checkOrUpdate(t, "task_list", buf.String())
}

// ── renderTaskGetCompact golden test ─────────────────────────────────────────

func TestCompactGolden_TaskGet_InProgress(t *testing.T) {
	tk := goldenTasks[0] // in_progress
	var buf bytes.Buffer
	renderTaskGetCompact(&buf, &tk)
	checkOrUpdate(t, "task_get_in_progress", buf.String())
}

func TestCompactGolden_TaskGet_Blocked(t *testing.T) {
	tk := store.Task{
		ID: "TASK-099", Title: "blocked feature", Status: "blocked",
		Priority: "high", BlockReason: "waiting on TASK-042 to ship first",
	}
	var buf bytes.Buffer
	renderTaskGetCompact(&buf, &tk)
	checkOrUpdate(t, "task_get_blocked", buf.String())
}

func TestCompactGolden_TaskGet_Done(t *testing.T) {
	tk := store.Task{
		ID: "TASK-003", Title: "write auth integration test", Status: "done",
		Priority: "low", DoneSummary: "all 12 scenarios pass, coverage 94%",
	}
	var buf bytes.Buffer
	renderTaskGetCompact(&buf, &tk)
	checkOrUpdate(t, "task_get_done", buf.String())
}

// ── writeInboxCompact golden test ────────────────────────────────────────────

func TestCompactGolden_InboxCompact(t *testing.T) {
	var buf bytes.Buffer
	writeInboxCompact(&buf, goldenMessages)
	checkOrUpdate(t, "inbox_compact", buf.String())
}

// ── renderForTaskCompact golden test ─────────────────────────────────────────

func TestCompactGolden_ForTaskCompact(t *testing.T) {
	tb := taskContextBundle{
		anchor: goldenTasks[0],
		deps:   goldenTasks[1:3],
		bundle: contextBundle{
			decisions:  goldenDecisions,
			rejections: goldenRejections,
		},
	}
	var buf bytes.Buffer
	renderForTaskCompact(&buf, tb, nil)
	checkOrUpdate(t, "for_task_compact", buf.String())
}

// ── BUG-027 regression: renderTaskGetDefault must always show Detail ──────────
//
// Agent auto-compact (GG_AGENT/GG_ROLE env) must not suppress the Detail block
// in 'gg task get'. Workers need the spec; the one-liner hid it. (BUG-027)

func TestBUG027_TaskGetDefault_ShowsDetail(t *testing.T) {
	tk := store.Task{
		ID:       "TASK-042",
		Title:    "implement JWT refresh endpoint",
		Status:   "pending",
		Priority: "high",
		Detail:   "AC-1: must return 401 on expired token\nAC-2: must issue new token within 200ms",
		Tags:     []string{"auth"},
	}
	var buf bytes.Buffer
	renderTaskGetDefault(&buf, &tk)
	out := buf.String()
	if !strings.Contains(out, "AC-1") {
		t.Errorf("renderTaskGetDefault: Detail block missing — got:\n%s", out)
	}
	if !strings.Contains(out, "AC-2") {
		t.Errorf("renderTaskGetDefault: Detail block truncated — got:\n%s", out)
	}
}

// TestAC1_TaskGetDefault_Detail verifies AC-4: default format contains the Detail block.
func TestAC1_TaskGetDefault_Detail(t *testing.T) {
	tk := store.Task{
		ID:       "TASK-338",
		Title:    "Fix BUG-027: gg task get default-format must show Detail block",
		Status:   "in_progress",
		Priority: "medium",
		Detail:   "AC-1: default shows header+Status+Detail\nAC-2: --short one line\nAC-3: --json unchanged",
	}
	var buf bytes.Buffer
	renderTaskGetDefault(&buf, &tk)
	out := buf.String()
	if !strings.Contains(out, "AC-1") {
		t.Errorf("default format: Detail block missing — got:\n%s", out)
	}
	if !strings.Contains(out, "TASK-338") {
		t.Errorf("default format: header line missing — got:\n%s", out)
	}
	if !strings.Contains(out, "in_progress") {
		t.Errorf("default format: Status line missing — got:\n%s", out)
	}
}

func TestBUG027_TaskGetCompact_OneLiner(t *testing.T) {
	tk := store.Task{
		ID:       "TASK-042",
		Title:    "implement JWT refresh endpoint",
		Status:   "pending",
		Priority: "high",
		Detail:   "AC-1: must return 401 on expired token\nAC-2: must issue new token within 200ms",
	}
	var buf bytes.Buffer
	renderTaskGetCompact(&buf, &tk)
	out := buf.String()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 1 {
		t.Errorf("renderTaskGetCompact: expected 1 line, got %d:\n%s", len(lines), out)
	}
	if strings.Contains(out, "AC-1") {
		t.Errorf("renderTaskGetCompact: Detail leaked into compact output:\n%s", out)
	}
}

// TestAC3_TaskGetJSON_Unchanged verifies that --json output is unaffected by the
// BUG-027 fix and still contains the Detail field.
func TestAC3_TaskGetJSON_Unchanged(t *testing.T) {
	tk := store.Task{
		ID:       "TASK-042",
		Title:    "implement JWT refresh endpoint",
		Status:   "pending",
		Priority: "high",
		Detail:   "AC-1: must return 401 on expired token\nAC-2: must issue new token within 200ms",
	}

	prev := jsonOutput
	jsonOutput = true
	t.Cleanup(func() { jsonOutput = prev })

	var buf bytes.Buffer
	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	_ = printJSON(&tk, func() {
		renderTaskGetDefault(&buf, &tk)
	})
	if err := w.Close(); err != nil {
		t.Fatalf("close stdout pipe: %v", err)
	}
	os.Stdout = origStdout

	var jsonBuf bytes.Buffer
	_, _ = jsonBuf.ReadFrom(r)
	out := jsonBuf.String()

	if !strings.Contains(out, `"Detail"`) && !strings.Contains(out, `"detail"`) {
		t.Errorf("--json output must contain Detail field, got:\n%s", out)
	}
	if !strings.Contains(out, "TASK-042") {
		t.Errorf("--json output must contain task ID, got:\n%s", out)
	}
	if buf.Len() != 0 {
		t.Errorf("--json must not invoke the fallback renderer, but it did")
	}
}

// TestAC2_TaskGetShort_Flag verifies the --short flag routes to the one-liner
// renderer (renderTaskGetCompact) and does not expose the Detail block.
func TestAC2_TaskGetShort_Flag(t *testing.T) {
	tk := store.Task{
		ID:       "TASK-042",
		Title:    "implement JWT refresh endpoint",
		Status:   "pending",
		Priority: "high",
		Detail:   "AC-1: must return 401 on expired token\nAC-2: must issue new token within 200ms",
	}

	// Save and restore global flag state so parallel tests are not affected.
	prev := taskGetShort
	taskGetShort = true
	t.Cleanup(func() { taskGetShort = prev })

	var buf bytes.Buffer
	renderTaskGetCompact(&buf, &tk)
	out := buf.String()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 1 {
		t.Errorf("--short: expected 1 line, got %d:\n%s", len(lines), out)
	}
	if strings.Contains(out, "AC-1") {
		t.Errorf("--short: Detail leaked into short output:\n%s", out)
	}
	if !strings.Contains(out, "TASK-042") {
		t.Errorf("--short: task ID missing from output:\n%s", out)
	}
}
