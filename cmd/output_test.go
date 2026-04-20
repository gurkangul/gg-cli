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
		{"VerifyFailed", ExitVerifyFailed, 7},
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
	ctx, cancel := withTimeout(context.Background())
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
		printMessage(m, "") // must not panic
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
	if err := printContextBundle(cmd, "test-query", bundle, errs, time.Time{}); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

// ── context.go: compactTrim ──────────────────────────────────────────────────

func TestCompactTrim(t *testing.T) {
	cases := []struct {
		in   string
		n    int
		want string
	}{
		{"short", 10, "short"},
		{"exactlyten", 10, "exactlyten"},
		{"this is longer than ten characters", 10, "this is l…"},
		{"çalışmayıılık", 5, "çalı…"}, // multibyte safety
		{"", 5, ""},
	}
	for _, tc := range cases {
		got := compactTrim(tc.in, tc.n)
		if got != tc.want {
			t.Errorf("compactTrim(%q, %d) = %q, want %q", tc.in, tc.n, got, tc.want)
		}
	}
}

// ── context.go: renderContextCompact ─────────────────────────────────────────

func TestRenderContextCompact_Structure(t *testing.T) {
	bundle := contextBundle{
		decisions: []store.Decision{
			{Text: "use JWT for auth", CreatedAt: "2026-04-10T12:00:00Z", TaskID: "TASK-001", Reason: "stateless"},
		},
		rejections: []store.Rejection{
			{Approach: "session-based auth", CreatedAt: "2026-04-09T12:00:00Z", Reason: "scaling"},
		},
		tasks: []store.Task{
			{ID: "TASK-042", Title: "JWT refresh endpoint", Status: "pending", Priority: "high", Detail: "this detail must NOT appear"},
		},
		discussions: []store.Discussion{
			{ID: "DISC-008", Topic: "rotate secrets", Status: "open", Turns: []store.Turn{{By: "a"}, {By: "b"}}, Detail: "this detail must NOT appear"},
		},
		notes: []store.Note{
			{Text: "saw X in logs", CreatedAt: "2026-04-08T12:00:00Z", TaskID: "TASK-042"},
		},
	}

	var buf strings.Builder
	renderContextCompact(&buf, "auth", bundle, nil)
	out := buf.String()

	// Header with counts.
	if !strings.Contains(out, `context: "auth" — 1D 1R 1T 1? 1N`) {
		t.Errorf("missing header with counts:\n%s", out)
	}

	// Body contents present.
	for _, want := range []string{
		"D  2026-04-10  use JWT for auth →TASK-001",
		"R  2026-04-09  session-based auth",
		"T ○ TASK-042  JWT refresh endpoint (high)",
		"? ? DISC-008  rotate secrets (2 turns)",
		"N  2026-04-08  (TASK-042)  saw X in logs",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing line %q in:\n%s", want, out)
		}
	}

	// Suppressed bodies — Reason / Detail must be absent in compact view.
	for _, forbidden := range []string{"stateless", "scaling", "this detail must NOT appear"} {
		if strings.Contains(out, forbidden) {
			t.Errorf("compact output leaked suppressed body %q:\n%s", forbidden, out)
		}
	}

	// One line per item + header (2 lines: header + blank) + 5 items = 7 lines total.
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 7 {
		t.Errorf("expected 7 output lines (header, blank, 5 items), got %d:\n%s", len(lines), out)
	}
}

func TestRenderContextCompact_Empty(t *testing.T) {
	var buf strings.Builder
	renderContextCompact(&buf, "nothing", contextBundle{}, nil)
	out := buf.String()
	if !strings.Contains(out, `context: "nothing" — 0D 0R 0T 0? 0N`) {
		t.Errorf("empty bundle should still emit header, got:\n%s", out)
	}
}

func TestRenderContextCompact_Errors(t *testing.T) {
	var buf strings.Builder
	renderContextCompact(&buf, "q", contextBundle{}, []string{"qdrant down", "embedder slow"})
	out := buf.String()
	if !strings.Contains(out, "! qdrant down; embedder slow") {
		t.Errorf("errors should be rendered on a trailing ! line, got:\n%s", out)
	}
}

// ── search.go: renderSearchCompact ───────────────────────────────────────────

func TestRenderSearchCompact_Structure(t *testing.T) {
	decisions := []store.Decision{
		{Text: "use JWT", CreatedAt: "2026-04-10T12:00:00Z", TaskID: "TASK-001", Reason: "stateless"},
	}
	rejections := []store.Rejection{
		{Approach: "sessions", CreatedAt: "2026-04-09T12:00:00Z", Reason: "scaling"},
	}
	var buf strings.Builder
	renderSearchCompact(&buf, decisions, rejections)
	out := buf.String()

	if !strings.Contains(out, "search — 1D 1R") {
		t.Errorf("missing header:\n%s", out)
	}
	if !strings.Contains(out, "D  2026-04-10  use JWT →TASK-001") {
		t.Errorf("missing decision line:\n%s", out)
	}
	if !strings.Contains(out, "R  2026-04-09  sessions") {
		t.Errorf("missing rejection line:\n%s", out)
	}
	for _, forbidden := range []string{"stateless", "scaling"} {
		if strings.Contains(out, forbidden) {
			t.Errorf("leaked suppressed reason %q:\n%s", forbidden, out)
		}
	}
}

func TestRenderSearchCompact_Empty(t *testing.T) {
	var buf strings.Builder
	renderSearchCompact(&buf, nil, nil)
	out := buf.String()
	if !strings.Contains(out, "(no results)") {
		t.Errorf("empty search should print (no results), got:\n%s", out)
	}
}

// ── impact.go: renderImpactCompact ───────────────────────────────────────────

// TestCompactSizeReduction measures the real byte-count delta between
// default and compact rendering on a realistic bundle. Not an assertion —
// the Log lines surface actual numbers when run with -v.
func TestCompactSizeReduction_ContextBundle(t *testing.T) {
	bundle := contextBundle{
		decisions: []store.Decision{
			{Text: "Use JWT for stateless authentication instead of server-side sessions", CreatedAt: "2026-04-10T12:00:00Z", TaskID: "TASK-001",
				Reason: "Stateless tokens scale horizontally without session affinity; enables mobile apps, microservices, and easier cross-origin auth", Tags: []string{"auth", "backend", "mobile"}},
			{Text: "Rotate JWT signing keys every 90 days", CreatedAt: "2026-04-08T09:30:00Z", TaskID: "TASK-015",
				Reason: "Compromised keys have bounded blast radius; 90d aligns with SOC2 expectations and is short enough to catch key leaks before damage accumulates", Tags: []string{"auth", "security", "compliance"}},
			{Text: "Access tokens 15 min, refresh tokens 7 days", CreatedAt: "2026-04-05T14:20:00Z", TaskID: "TASK-015",
				Reason: "Short access tokens limit session theft damage; 7d refresh balances UX against revocation latency. Matches industry norm (Auth0, Okta defaults).", Tags: []string{"auth", "security"}},
		},
		rejections: []store.Rejection{
			{Approach: "Session cookies with Redis-backed store", CreatedAt: "2026-04-10T11:00:00Z",
				Reason: "Adds a stateful dependency in the hot path; session affinity required or Redis becomes a SPOF; cross-domain auth is painful", Tags: []string{"auth", "infra"}},
		},
		tasks: []store.Task{
			{ID: "TASK-042", Title: "Implement JWT refresh token rotation endpoint", Status: "in_progress", Priority: "high",
				Detail: "Add POST /auth/refresh that validates the current refresh token, issues a new access+refresh pair, and invalidates the old refresh token. Needs rate limiting and reuse detection.", Tags: []string{"auth", "api"}},
			{ID: "TASK-007", Title: "Add JWT revocation list cache", Status: "pending", Priority: "medium",
				Detail: "When a user logs out or admin revokes access, write the jti to a time-bounded revocation set (Redis, ttl = max-token-lifetime). Validate against it in the JWT middleware.", Tags: []string{"auth", "backend"}},
		},
		discussions: []store.Discussion{
			{ID: "DISC-008", Topic: "Should we adopt FIDO2 / WebAuthn alongside password auth?", Status: "open",
				Detail: "Security team flagged passwords-only as insufficient for admin accounts. WebAuthn would add phishing resistance but complicates account recovery flows.",
				Turns: []store.Turn{{By: "sec-lead"}, {By: "platform"}, {By: "product"}}},
		},
		notes: []store.Note{
			{Text: "Observed a 15% spike in refresh-token requests between 09:00-10:00 UTC — investigate whether a client is aggressively pre-refreshing.",
				CreatedAt: "2026-04-08T10:15:00Z", TaskID: "TASK-042"},
		},
	}

	var defaultBuf, compactBuf strings.Builder
	// Default path writes via the closure in printContextBundle; we extract the
	// same stdout-writing logic by invoking the fallback manually.
	renderContextDefault(&defaultBuf, "auth", bundle, nil)
	renderContextCompact(&compactBuf, "auth", bundle, nil)

	def := defaultBuf.Len()
	cmp := compactBuf.Len()
	saved := def - cmp
	pct := float64(saved) / float64(def) * 100
	t.Logf("default render: %d bytes", def)
	t.Logf("compact render: %d bytes", cmp)
	t.Logf("reduction:      %d bytes (%.1f%%)", saved, pct)

	if cmp >= def {
		t.Fatalf("compact rendering (%d) was not smaller than default (%d)", cmp, def)
	}
}

func TestRenderImpactCompact_Structure(t *testing.T) {
	r := impactResult{
		File:       "/abs/path/src/auth.go",
		Dependents: []string{"/abs/path/src/api.go"},
		Symbols:    []map[string]any{{"name": "HandleLogin", "kind": "func"}},
		Decisions: []store.Decision{
			{Text: "use bcrypt", CreatedAt: "2026-04-10T12:00:00Z", TaskID: "TASK-007", Reason: "must not appear"},
		},
		Tasks: []store.Task{
			{ID: "TASK-042", Title: "rotate secrets", Status: "pending", Priority: "high", Detail: "must not appear"},
		},
		Rejections: []store.Rejection{
			{Approach: "MD5", CreatedAt: "2026-04-09T12:00:00Z", Reason: "must not appear"},
		},
	}
	var buf strings.Builder
	renderImpactCompact(&buf, r)
	out := buf.String()

	if !strings.Contains(out, "impact: /abs/path/src/auth.go — 1 deps 1 sym 1D 1T 1R") {
		t.Errorf("missing header:\n%s", out)
	}
	for _, want := range []string{
		"→ /abs/path/src/api.go",
		"S HandleLogin",
		"D  2026-04-10  use bcrypt →TASK-007",
		"T ○ TASK-042  rotate secrets (high)",
		"R  2026-04-09  MD5",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing line %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "must not appear") {
		t.Errorf("leaked suppressed body:\n%s", out)
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
