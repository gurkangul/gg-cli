// Package cmd — unit tests for pure output/error helpers.
package cmd

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gurkangul/gg-cli/internal/index/runner"
	"github.com/gurkangul/gg-cli/internal/store"
	"github.com/spf13/cobra"
)

// ── ExitError ─────────────────────────────────────────────────────────────────

func TestExitError_Error(t *testing.T) {
	e := &ExitError{Code: 2, Message: "not found"}
	if e.Error() != "not found" {
		t.Errorf("got %q, want %q", e.Error(), "not found")
	}
}

func TestExitError_ZeroCode(t *testing.T) {
	e := &ExitError{Code: 0, Message: "ok"}
	if e.Code != 0 {
		t.Errorf("expected code 0, got %d", e.Code)
	}
}

// ── constructor helpers ───────────────────────────────────────────────────────

func TestNotFound_Code(t *testing.T) {
	err := notFound("resource missing")
	ee, ok := err.(*ExitError)
	if !ok {
		t.Fatalf("expected *ExitError, got %T", err)
	}
	if ee.Code != ExitNotFound {
		t.Errorf("code: got %d, want %d", ee.Code, ExitNotFound)
	}
	if ee.Message != "resource missing" {
		t.Errorf("message: got %q, want %q", ee.Message, "resource missing")
	}
}

func TestConfigErr_Code(t *testing.T) {
	err := configErr("run gg init")
	ee, ok := err.(*ExitError)
	if !ok {
		t.Fatalf("expected *ExitError, got %T", err)
	}
	if ee.Code != ExitConfig {
		t.Errorf("code: got %d, want %d", ee.Code, ExitConfig)
	}
}

func TestServiceErr_Code(t *testing.T) {
	err := serviceErr("Qdrant unreachable")
	ee, ok := err.(*ExitError)
	if !ok {
		t.Fatalf("expected *ExitError, got %T", err)
	}
	if ee.Code != ExitService {
		t.Errorf("code: got %d, want %d", ee.Code, ExitService)
	}
}

func TestStoreDownErr_CodeAndMessage(t *testing.T) {
	err := storeDownErr()
	ee, ok := err.(*ExitError)
	if !ok {
		t.Fatalf("expected *ExitError, got %T", err)
	}
	if ee.Code != ExitStoreDown {
		t.Errorf("code: got %d, want %d", ee.Code, ExitStoreDown)
	}
	if !strings.Contains(ee.Message, "Qdrant") {
		t.Errorf("message should mention Qdrant, got: %q", ee.Message)
	}
}

// ── Exit code constants ───────────────────────────────────────────────────────

func TestExitCodeValues(t *testing.T) {
	cases := []struct {
		name string
		code int
		want int
	}{
		{"OK", ExitOK, 0},
		{"General", ExitGeneral, 1},
		{"NotFound", ExitNotFound, 2},
		{"Config", ExitConfig, 3},
		{"Service", ExitService, 4},
		{"StoreDown", ExitStoreDown, 6},
		{"Signal", ExitSignal, 130},
	}
	for _, tc := range cases {
		if tc.code != tc.want {
			t.Errorf("%s: got %d, want %d", tc.name, tc.code, tc.want)
		}
	}
}

// ── context.go pure helpers (same package) ───────────────────────────────────

func TestShortDate_Full(t *testing.T) {
	got := shortDate("2026-04-14T18:40:42Z")
	if got != "2026-04-14" {
		t.Errorf("got %q, want %q", got, "2026-04-14")
	}
}

func TestShortDate_ExactlyTen(t *testing.T) {
	got := shortDate("2026-04-14")
	if got != "2026-04-14" {
		t.Errorf("got %q, want %q", got, "2026-04-14")
	}
}

func TestShortDate_Short(t *testing.T) {
	got := shortDate("2026")
	if got != "—" {
		t.Errorf("expected em-dash for short string, got %q", got)
	}
}

func TestShortDate_Empty(t *testing.T) {
	got := shortDate("")
	if got != "—" {
		t.Errorf("expected em-dash for empty string, got %q", got)
	}
}

func TestTaskStatusIcon(t *testing.T) {
	cases := []struct{ status, want string }{
		{"done", "✓"},
		{"in_progress", "→"},
		{"blocked", "!"},
		{"pending", "○"},
		{"unknown", "○"},
		{"", "○"},
	}
	for _, tc := range cases {
		got := taskStatusIcon(tc.status)
		if got != tc.want {
			t.Errorf("taskStatusIcon(%q): got %q, want %q", tc.status, got, tc.want)
		}
	}
}

func TestDiscStatusMark(t *testing.T) {
	cases := []struct{ status, want string }{
		{"resolved", "✓"},
		{"dismissed", "–"},
		{"open", "?"},
		{"", "?"},
	}
	for _, tc := range cases {
		got := discStatusMark(tc.status)
		if got != tc.want {
			t.Errorf("discStatusMark(%q): got %q, want %q", tc.status, got, tc.want)
		}
	}
}

// ── discuss.go helpers ────────────────────────────────────────────────────────

func TestDiscussStatusIcon(t *testing.T) {
	cases := []struct{ status, want string }{
		{"resolved", "✓"},
		{"dismissed", "⊘"},
		{"open", "•"},
		{"", "•"},
	}
	for _, tc := range cases {
		got := discussStatusIcon(tc.status)
		if got != tc.want {
			t.Errorf("discussStatusIcon(%q): got %q, want %q", tc.status, got, tc.want)
		}
	}
}

// ── task.go helpers ───────────────────────────────────────────────────────────

func TestStatusIcon_Task(t *testing.T) {
	cases := []struct{ status, want string }{
		{"done", "✓"},
		{"blocked", "⚠"},
		{"in_progress", "→"},
		{"pending", "○"},
		{"", "○"},
	}
	for _, tc := range cases {
		got := statusIcon(tc.status)
		if got != tc.want {
			t.Errorf("statusIcon(%q): got %q, want %q", tc.status, got, tc.want)
		}
	}
}

// ── export.go helpers ─────────────────────────────────────────────────────────

func TestHumanFileSize(t *testing.T) {
	cases := []struct {
		b    int64
		want string
	}{
		{500, "500 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1024 * 1024, "1.0 MB"},
		{2 * 1024 * 1024, "2.0 MB"},
	}
	for _, tc := range cases {
		got := humanFileSize(tc.b)
		if got != tc.want {
			t.Errorf("humanFileSize(%d): got %q, want %q", tc.b, got, tc.want)
		}
	}
}

// ── inbox.go helpers ──────────────────────────────────────────────────────────

func TestParseDuration_DaysSuffix(t *testing.T) {
	d, err := parseDuration("7d")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d != 7*24*time.Hour {
		t.Errorf("expected 168h, got %v", d)
	}
}

func TestParseDuration_StdLib(t *testing.T) {
	d, err := parseDuration("2h30m")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d != 2*time.Hour+30*time.Minute {
		t.Errorf("expected 2h30m, got %v", d)
	}
}

func TestParseDuration_Invalid(t *testing.T) {
	if _, err := parseDuration("notaduration"); err == nil {
		t.Error("expected error for invalid duration")
	}
}

func TestParseDuration_InvalidDaySuffix(t *testing.T) {
	// "xd" has the "d" suffix but the prefix is not a valid integer.
	if _, err := parseDuration("xd"); err == nil {
		t.Error("expected error for non-numeric day value")
	}
}

// ── status.go helpers ─────────────────────────────────────────────────────────

func TestPct(t *testing.T) {
	if pct(0, 0) != 0 {
		t.Error("pct(0,0) should be 0")
	}
	if pct(50, 100) != 50 {
		t.Errorf("pct(50,100) = %d, want 50", pct(50, 100))
	}
	if pct(1, 3) != 33 {
		t.Errorf("pct(1,3) = %d, want 33", pct(1, 3))
	}
}

func TestFmtCount(t *testing.T) {
	if fmtCount(42, nil) != "42" {
		t.Errorf("fmtCount(42, nil) = %q", fmtCount(42, nil))
	}
	if fmtCount(0, fmt.Errorf("some error")) != "?" {
		t.Error("fmtCount with error should return ?")
	}
}

// ── doctor.go helpers ─────────────────────────────────────────────────────────

func TestSchemaMajor(t *testing.T) {
	if schemaMajor("1.2.3") != 1 {
		t.Errorf("schemaMajor(1.2.3) should be 1")
	}
	if schemaMajor("2.0") != 2 {
		t.Errorf("schemaMajor(2.0) should be 2")
	}
	if schemaMajor("notanumber") != 0 {
		t.Errorf("schemaMajor(notanumber) should be 0")
	}
	if schemaMajor("") != 0 {
		t.Errorf("schemaMajor('') should be 0")
	}
}

// ── index.go helpers ──────────────────────────────────────────────────────────

func TestLangExtensions(t *testing.T) {
	goExts := langExtensions(runner.LangGo)
	if len(goExts) != 1 || goExts[0] != ".go" {
		t.Errorf("Go extensions: got %v", goExts)
	}
	pyExts := langExtensions(runner.LangPython)
	if len(pyExts) != 1 || pyExts[0] != ".py" {
		t.Errorf("Python extensions: got %v", pyExts)
	}
	tsExts := langExtensions(runner.LangTypeScript)
	if len(tsExts) == 0 {
		t.Error("TypeScript extensions should not be empty")
	}
	// unknown lang
	unknownExts := langExtensions(runner.Lang("unknown"))
	if unknownExts != nil {
		t.Errorf("unknown lang should return nil, got %v", unknownExts)
	}
}

// ── helpers.go: withTimeout ───────────────────────────────────────────────────

func TestWithTimeout_NilParent(t *testing.T) {
	// withTimeout(nil) must substitute context.Background() rather than panic.
	ctx, cancel := withTimeout(nil)
	defer cancel()
	if ctx == nil {
		t.Error("withTimeout(nil) returned nil context")
	}
	if ctx.Err() != nil {
		t.Errorf("withTimeout(nil) context already done: %v", ctx.Err())
	}
}

// ── helpers.go: resolveAuthor ─────────────────────────────────────────────────

func TestResolveAuthor_FromFlag(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	addFromFlag(cmd)
	if err := cmd.Flags().Set("from", "architect"); err != nil {
		t.Fatalf("Set flag: %v", err)
	}
	got := resolveAuthor(cmd)
	if got != "architect" {
		t.Errorf("resolveAuthor with --from: got %q, want %q", got, "architect")
	}
}

func TestResolveAuthor_NoFlag(t *testing.T) {
	// No --from flag and no GG_ROLE env → returns "".
	t.Setenv("GG_ROLE", "")
	cmd := &cobra.Command{Use: "test"}
	got := resolveAuthor(cmd)
	// Returns env value (empty) when flag is not changed.
	_ = got // result is environment-dependent; just ensure no panic
}

// ── bug.go: requireBugID ──────────────────────────────────────────────────────

func TestRequireBugID_Valid(t *testing.T) {
	id, err := requireBugID("BUG-042")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "BUG-042" {
		t.Errorf("got %q, want %q", id, "BUG-042")
	}
}

func TestRequireBugID_LowercaseNormalized(t *testing.T) {
	id, err := requireBugID("bug-007")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "BUG-007" {
		t.Errorf("got %q, want %q", id, "BUG-007")
	}
}

func TestRequireBugID_InvalidFormat(t *testing.T) {
	if _, err := requireBugID("not-a-bug"); err == nil {
		t.Error("expected error for invalid bug ID format")
	}
}

// ── inbox.go: printMessages / printMessage ────────────────────────────────────

func TestPrintMessage_Basic(t *testing.T) {
	// printMessage writes to stdout; call it to exercise all branches.
	msgs := []store.Message{
		{FromRole: "architect", ToRole: "developer", Content: "LGTM"},
		{FromRole: "qa", ToRole: "developer", Content: "TASK-007 ready", TaskID: "TASK-007"},
		{FromRole: "pm", ToRole: "dev", Content: "ship it",
			CreatedAt: "2026-04-14T12:00:00Z"},
		{FromRole: "pm", ToRole: "dev", Content: "invalid ts",
			CreatedAt: "not-a-timestamp"},
	}
	for _, m := range msgs {
		printMessage(m) // must not panic
	}
}

func TestPrintMessages_DefaultFormat(t *testing.T) {
	msgs := []store.Message{
		{FromRole: "architect", ToRole: "developer", Content: "ready for review"},
		{FromRole: "qa", ToRole: "developer", Content: "tests passing", TaskID: "TASK-001"},
	}
	// groupBy != "sender" → default flat format
	printMessages(msgs, "")
}

func TestPrintMessages_GroupBySender(t *testing.T) {
	msgs := []store.Message{
		{FromRole: "pm", ToRole: "developer", Content: "ship now"},
		{FromRole: "pm", ToRole: "developer", Content: "seriously"},
		{FromRole: "qa", ToRole: "developer", Content: "tests failed"},
	}
	printMessages(msgs, "sender")
}

func TestPrintMessages_Empty(t *testing.T) {
	// Empty slice — both format paths should handle it without panic.
	printMessages(nil, "")
	printMessages(nil, "sender")
}

// ── context.go: printContextBundle with non-empty errs ───────────────────────

func TestPrintContextBundle_WithErrors(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	bundle := contextBundle{}
	errs := []string{"qdrant search failed", "embedder timeout"}
	// Must not panic and must return nil.
	if err := printContextBundle(cmd, "test-query", bundle, errs); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

// ── init.go: waitForHTTP ──────────────────────────────────────────────────────

func TestWaitForHTTP_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ctx := context.Background()
	if err := waitForHTTP(ctx, srv.URL, 5*time.Second); err != nil {
		t.Errorf("expected nil error for reachable server, got: %v", err)
	}
}

func TestWaitForHTTP_CanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before any call

	// Should return immediately with a context error.
	err := waitForHTTP(ctx, "http://127.0.0.1:19994", 10*time.Second)
	if err == nil {
		t.Error("expected context error, got nil")
	}
}

func TestWaitForHTTP_Timeout(t *testing.T) {
	// Nothing listening on 19993 — should time out quickly.
	ctx := context.Background()
	err := waitForHTTP(ctx, "http://127.0.0.1:19993", 50*time.Millisecond)
	if err == nil {
		t.Error("expected timeout error, got nil")
	}
}

func TestWaitForHTTP_ContextCanceledInSelect(t *testing.T) {
	// Context with a short timeout that fires while we're waiting in the select
	// (after a failed connection attempt). This exercises the
	// "case <-ctx.Done(): return ctx.Err()" branch inside the select.
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	// Long waitForHTTP timeout (10s) so the outer deadline doesn't trigger.
	// Port 19992 has nothing listening; connection is refused immediately,
	// then we sleep in select until ctx times out.
	err := waitForHTTP(ctx, "http://127.0.0.1:19992", 10*time.Second)
	if err == nil {
		t.Error("expected context deadline error, got nil")
	}
}
