// Package cmd — tests for gg master resume command.
package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gurkangul/gg-cli/internal/orchestrator/spawn"
	"github.com/gurkangul/gg-cli/internal/store"
)

// TestMasterResume_NoGGDir verifies that the command returns a config error
// when there is no .gg directory in the working directory.
func TestMasterResume_NoGGDir(t *testing.T) {
	t.Chdir(t.TempDir())
	_, _, err := execCmd(t, "master", "resume")
	if err == nil {
		t.Fatal("expected error when .gg config is absent")
	}
}

// TestMasterResume_QdrantDown verifies that the command succeeds and prints
// local sections (git log, spawn state) even when Qdrant is unreachable.
func TestMasterResume_QdrantDown(t *testing.T) {
	setupGGDir(t)
	stdout, _, err := execCmd(t, "master", "resume")
	if err != nil {
		t.Fatalf("master resume should not fail when Qdrant is down: %v", err)
	}
	// Git log section must appear.
	if !strings.Contains(stdout, "Recent Commits") {
		t.Errorf("output missing 'Recent Commits' section; got:\n%s", stdout)
	}
	// Spawn state sections must appear.
	if !strings.Contains(stdout, "Master Liveness") {
		t.Errorf("output missing 'Master Liveness' section; got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "Queue Session") {
		t.Errorf("output missing 'Queue Session' section; got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "Active Worker Panes") {
		t.Errorf("output missing 'Active Worker Panes' section; got:\n%s", stdout)
	}
	// Qdrant-backed sections must be gated with a note, not panic.
	if !strings.Contains(stdout, "Qdrant") {
		t.Errorf("output should mention Qdrant unavailability; got:\n%s", stdout)
	}
}

// TestMasterResume_NoHeartbeat_NoPanes verifies the empty-state rendering
// (no heartbeat.json, no panes.json) does not panic and produces coherent output.
func TestMasterResume_NoHeartbeat_NoPanes(t *testing.T) {
	setupGGDir(t)
	stdout, _, err := execCmd(t, "master", "resume")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout, "No heartbeat") {
		t.Errorf("expected 'No heartbeat' message; got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "No active queue session") {
		t.Errorf("expected 'No active queue session' message; got:\n%s", stdout)
	}
}

// TestMasterResume_WithHeartbeat verifies that an existing heartbeat.json is
// read and the liveness section renders the agent name and age.
func TestMasterResume_WithHeartbeat(t *testing.T) {
	setupGGDir(t)
	rt := testRuntimeDir(t)
	spawnDir := filepath.Join(rt, "spawn")
	if err := os.MkdirAll(spawnDir, 0o755); err != nil {
		t.Fatalf("mkdir spawn: %v", err)
	}
	hb := spawn.Heartbeat{
		PID:       42,
		Agent:     "claude-code",
		UpdatedAt: time.Now().Add(-30 * time.Second),
	}
	raw, _ := json.Marshal(hb)
	if err := os.WriteFile(filepath.Join(spawnDir, spawn.HeartbeatFile), raw, 0o644); err != nil {
		t.Fatalf("write heartbeat: %v", err)
	}

	stdout, _, err := execCmd(t, "master", "resume")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout, "claude-code") {
		t.Errorf("expected agent name in liveness section; got:\n%s", stdout)
	}
}

// TestBuildMasterResumeJSON_StableArrays verifies that the JSON builder
// produces empty arrays (not null) for all nil slice inputs, and that
// the serialised JSON round-trips correctly.
func TestBuildMasterResumeJSON_StableArrays(t *testing.T) {
	payload := buildMasterResumeJSON(nil, nil, false, nil, nil, nil, nil, nil, nil, nil)

	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	for _, key := range []string{"git_log", "workers", "pending_tasks", "ready_for_live", "inbox_messages", "recent_decisions"} {
		v, ok := out[key]
		if !ok {
			t.Errorf("JSON missing key %q", key)
			continue
		}
		if v == nil {
			t.Errorf("JSON key %q is null, want []", key)
		}
		if _, isSlice := v.([]any); !isSlice {
			t.Errorf("JSON key %q is %T, want []any", key, v)
		}
	}
	if _, ok := out["master_alive"]; !ok {
		t.Error("JSON missing 'master_alive'")
	}
}

// ── recentGitLog unit tests ───────────────────────────────────────────────────

// TestRecentGitLog_NotInRepo verifies that recentGitLog returns without panicking
// when the working directory is not a git repository.
func TestRecentGitLog_NotInRepo(t *testing.T) {
	t.Chdir(t.TempDir())
	// Must not panic regardless of result.
	lines := recentGitLog()
	_ = lines
}

// ── buildMasterResumeJSON unit tests ─────────────────────────────────────────

// TestBuildMasterResumeJSON_NilSlices verifies that nil slices produce empty
// JSON arrays rather than JSON null values.
func TestBuildMasterResumeJSON_NilSlices(t *testing.T) {
	out := buildMasterResumeJSON(
		nil, nil, false, nil, nil, nil, nil, nil, nil, nil,
	)
	sliceFields := []string{"git_log", "workers", "pending_tasks", "ready_for_live", "inbox_messages", "recent_decisions"}
	for _, field := range sliceFields {
		v, ok := out[field]
		if !ok {
			t.Errorf("missing field %q", field)
			continue
		}
		if v == nil {
			t.Errorf("field %q is nil, want empty slice", field)
		}
	}
}

// TestBuildMasterResumeJSON_HeartbeatIncluded verifies that a non-nil heartbeat
// is present in the output map.
func TestBuildMasterResumeJSON_HeartbeatIncluded(t *testing.T) {
	hb := &spawn.Heartbeat{Agent: "opus", PID: 1}
	out := buildMasterResumeJSON(nil, hb, true, nil, nil, nil, nil, nil, nil, nil)
	if _, ok := out["heartbeat"]; !ok {
		t.Error("heartbeat field missing when heartbeat is non-nil")
	}
	if _, ok := out["master_alive"]; !ok {
		t.Error("master_alive field missing")
	}
}

// TestBuildMasterResumeJSON_QueueSessionIncluded verifies that a non-nil queue
// session appears in the output map.
func TestBuildMasterResumeJSON_QueueSessionIncluded(t *testing.T) {
	sess := &spawn.QueueSession{Agent: "gsd", StartedAt: time.Now()}
	out := buildMasterResumeJSON(nil, nil, false, sess, nil, nil, nil, nil, nil, nil)
	if _, ok := out["queue_session"]; !ok {
		t.Error("queue_session field missing when session is non-nil")
	}
}

// ── printMasterResume unit tests ─────────────────────────────────────────────

// TestPrintMasterResume_ShortCreatedAt verifies that a decision with a
// CreatedAt shorter than 10 chars does not panic (uses shortDate safely).
func TestPrintMasterResume_ShortCreatedAt(t *testing.T) {
	decisions := []store.Decision{
		{Text: "some decision", CreatedAt: "2026"},
	}
	var buf strings.Builder
	// Must not panic.
	printMasterResume(&buf,
		nil,
		nil, spawn.ErrNoHeartbeat, false, "",
		nil, spawn.ErrNoQueue,
		nil, nil,
		nil, nil, nil, decisions,
		"",
		"", nil, nil,
	)
	if !strings.Contains(buf.String(), "some decision") {
		t.Errorf("expected decision text in output; got:\n%s", buf.String())
	}
}

// TestPrintMasterResume_EmptyDecisionCreatedAt verifies empty CreatedAt is safe.
func TestPrintMasterResume_EmptyDecisionCreatedAt(t *testing.T) {
	decisions := []store.Decision{
		{Text: "another decision", CreatedAt: ""},
	}
	var buf strings.Builder
	printMasterResume(&buf,
		nil,
		nil, spawn.ErrNoHeartbeat, false, "",
		nil, spawn.ErrNoQueue,
		nil, nil,
		nil, nil, nil, decisions,
		"",
		"", nil, nil,
	)
	if !strings.Contains(buf.String(), "another decision") {
		t.Errorf("expected decision text in output; got:\n%s", buf.String())
	}
}

// TestPrintMasterResume_QdrantNoteHidesQdrantSections verifies that when a
// qdrantNote is set, Qdrant-backed sections are not rendered and the note is.
func TestPrintMasterResume_QdrantNoteHidesQdrantSections(t *testing.T) {
	tasks := []store.Task{{ID: "TASK-001", Title: "should not appear", Status: "pending", Priority: "high"}}
	var buf strings.Builder
	printMasterResume(&buf,
		nil,
		nil, spawn.ErrNoHeartbeat, false, "",
		nil, spawn.ErrNoQueue,
		nil, nil,
		tasks, nil, nil, nil,
		"(Qdrant unreachable — tasks/inbox/decisions unavailable)",
		"", nil, nil,
	)
	out := buf.String()
	if strings.Contains(out, "TASK-001") {
		t.Error("pending task should not appear when qdrantNote is set")
	}
	if !strings.Contains(out, "Qdrant unreachable") {
		t.Errorf("expected qdrant note in output; got:\n%s", out)
	}
}

// TestPrintMasterResume_PanesJSONRaw_Present verifies that raw panes.json
// content (section 7) is printed verbatim when the file exists.
func TestPrintMasterResume_PanesJSONRaw_Present(t *testing.T) {
	rawContent := `[{"task_id":"TASK-042","surface_id":"gsd-1","agent":"claude"}]`
	var buf strings.Builder
	printMasterResume(&buf,
		nil,
		nil, spawn.ErrNoHeartbeat, false, "",
		nil, spawn.ErrNoQueue,
		nil, nil,
		nil, nil, nil, nil,
		"",
		"/tmp/fake/panes.json", []byte(rawContent), nil,
	)
	out := buf.String()
	if !strings.Contains(out, "panes.json raw") {
		t.Errorf("expected 'panes.json raw' section header; got:\n%s", out)
	}
	if !strings.Contains(out, "TASK-042") {
		t.Errorf("expected raw panes content in output; got:\n%s", out)
	}
}

// TestPrintMasterResume_PanesJSONRaw_Absent verifies that a missing panes.json
// prints the "file absent" message rather than an error.
func TestPrintMasterResume_PanesJSONRaw_Absent(t *testing.T) {
	var buf strings.Builder
	printMasterResume(&buf,
		nil,
		nil, spawn.ErrNoHeartbeat, false, "",
		nil, spawn.ErrNoQueue,
		nil, nil,
		nil, nil, nil, nil,
		"",
		"/tmp/fake/panes.json", nil, os.ErrNotExist,
	)
	out := buf.String()
	if !strings.Contains(out, "file absent") {
		t.Errorf("expected 'file absent' message; got:\n%s", out)
	}
}

// TestPrintMasterResume_InboxTop10 verifies that when runMasterResume caps
// messages to 10 before passing them to printMasterResume, only 10 appear.
// The cap lives in runMasterResume (allMessages[:10]); printMasterResume prints
// whatever slice it receives. This test documents that contract.
func TestPrintMasterResume_InboxTop10(t *testing.T) {
	all := make([]store.Message, 15)
	for i := range all {
		all[i] = store.Message{
			FromRole: "developer",
			ToRole:   "claude-code",
			Content:  fmt.Sprintf("message-%d", i),
		}
	}
	// Simulate what runMasterResume does: cap to 10.
	msgs := all[:10]

	var buf strings.Builder
	printMasterResume(&buf,
		nil,
		nil, spawn.ErrNoHeartbeat, false, "",
		nil, spawn.ErrNoQueue,
		nil, nil,
		nil, nil, msgs, nil,
		"",
		"", nil, nil,
	)
	out := buf.String()
	// First 10 messages must appear.
	for i := 0; i < 10; i++ {
		if !strings.Contains(out, fmt.Sprintf("message-%d", i)) {
			t.Errorf("expected message-%d in output", i)
		}
	}
	// Messages 10-14 must NOT appear (they were excluded by the cap).
	for i := 10; i < 15; i++ {
		if strings.Contains(out, fmt.Sprintf("message-%d", i)) {
			t.Errorf("unexpected message-%d in output (should be capped at 10)", i)
		}
	}
}

// TestBuildMasterResumeJSON_PanesRaw verifies that raw panes content is present
// in the JSON output map as a string field.
func TestBuildMasterResumeJSON_PanesRaw(t *testing.T) {
	rawContent := []byte(`[{"task_id":"TASK-042"}]`)
	out := buildMasterResumeJSON(nil, nil, false, nil, nil, nil, nil, nil, nil, rawContent)
	v, ok := out["panes_json_raw"]
	if !ok {
		t.Fatal("panes_json_raw field missing from JSON output")
	}
	s, ok := v.(string)
	if !ok {
		t.Fatalf("panes_json_raw should be string, got %T", v)
	}
	if !strings.Contains(s, "TASK-042") {
		t.Errorf("panes_json_raw missing expected content; got: %q", s)
	}
}
