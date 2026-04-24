package telemetry

import (
	"os"
	"testing"
	"time"
)

// TestMain enables telemetry for the entire test package so tests that call
// Record() and expect writes don't need individual setup. Tests that verify
// disabled behaviour override with t.Setenv("GG_TELEMETRY", "0").
func TestMain(m *testing.M) {
	SetEnabled(true)
	os.Exit(m.Run())
}

func TestRecord_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	Record(dir, "status", "")

	data, err := os.ReadFile(filePath(dir))
	if err != nil {
		t.Fatalf("telemetry file not created: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("telemetry file is empty")
	}
}

func TestRecord_DisabledViaEnv(t *testing.T) {
	for _, val := range []string{"0", "false", "FALSE", "no", "off", " 0 "} {
		t.Run("GG_TELEMETRY="+val, func(t *testing.T) {
			t.Setenv("GG_TELEMETRY", val)
			if !IsDisabled() {
				t.Fatalf("IsDisabled() = false, want true for %q", val)
			}
			dir := t.TempDir()
			Record(dir, "status", "")
			if _, err := os.ReadFile(filePath(dir)); !os.IsNotExist(err) {
				t.Errorf("telemetry file should not be created when disabled (val=%q): err=%v", val, err)
			}
		})
	}
}

// TestRecord_DisabledViaConfig — user writes `telemetry.enabled: false` in
// .gg/config.yaml → SetEnabled(false) is called → IsDisabled() returns true
// even without GG_TELEMETRY env override.
func TestRecord_DisabledViaConfig(t *testing.T) {
	t.Setenv("GG_TELEMETRY", "")
	SetEnabled(false)
	t.Cleanup(func() { SetEnabled(true) })

	if !IsDisabled() {
		t.Fatal("IsDisabled() = false, want true when config explicitly disables")
	}
	dir := t.TempDir()
	Record(dir, "status", "")
	if _, err := os.ReadFile(filePath(dir)); !os.IsNotExist(err) {
		t.Errorf("telemetry file should not be created when config disables: err=%v", err)
	}
}

// TestRecord_EnabledByDefault_BUG018 — telemetry is ON by default when neither
// GG_TELEMETRY nor config explicitly disables it. Regression for BUG-018:
// opt-in default silently killed the North Star dogfood metric because users
// never knew to set `telemetry.enabled: true`.
func TestRecord_EnabledByDefault_BUG018(t *testing.T) {
	t.Setenv("GG_TELEMETRY", "") // no env override
	// Simulate fresh process: SetEnabled never called. We can't un-call
	// SetEnabled in-process, so reset to the zero-state explicitly.
	configExplicitDisabled.Store(false)

	if IsDisabled() {
		t.Fatal("IsDisabled() = true, want false when neither env nor config opts out")
	}
	dir := t.TempDir()
	Record(dir, "status", "")
	data, err := os.ReadFile(filePath(dir))
	if err != nil {
		t.Fatalf("telemetry file should be created by default: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("telemetry file empty despite default-on semantics")
	}
}

func TestRecord_EnabledViaEnv(t *testing.T) {
	t.Setenv("GG_TELEMETRY", "1")
	SetEnabled(false) // config says off — env should override
	t.Cleanup(func() { SetEnabled(true) })

	if IsDisabled() {
		t.Fatal("IsDisabled() = true, want false when GG_TELEMETRY=1")
	}
	dir := t.TempDir()
	Record(dir, "status", "")
	if _, err := os.ReadFile(filePath(dir)); err != nil {
		t.Errorf("telemetry file should be created when GG_TELEMETRY=1: %v", err)
	}
}

func TestRecord_EnabledViaConfig(t *testing.T) {
	t.Setenv("GG_TELEMETRY", "") // env unset — rely on config
	// SetEnabled(true) is already set by TestMain

	if IsDisabled() {
		t.Fatal("IsDisabled() = true, want false when config enabled=true")
	}
	dir := t.TempDir()
	Record(dir, "status", "")
	if _, err := os.ReadFile(filePath(dir)); err != nil {
		t.Errorf("telemetry file should be created when config enables it: %v", err)
	}
}

func TestRecord_EmptyInputsNoOp(t *testing.T) {
	dir := t.TempDir()
	Record("", "status", "")    // empty ggDir
	Record(dir, "", "")         // empty verb
	Record("", "", "")

	_, err := os.ReadFile(filePath(dir))
	if !os.IsNotExist(err) {
		t.Error("expected no file to be created for empty inputs")
	}
}

func TestRecord_AppendsMultiple(t *testing.T) {
	dir := t.TempDir()
	Record(dir, "status", "")
	Record(dir, "search", "")
	Record(dir, "record", "")

	sum, err := Summarize(dir)
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if sum.Total != 3 {
		t.Errorf("expected 3 entries, got %d", sum.Total)
	}
	if sum.VerbCounts["status"] != 1 || sum.VerbCounts["search"] != 1 || sum.VerbCounts["record"] != 1 {
		t.Errorf("verb counts wrong: %v", sum.VerbCounts)
	}
}

func TestSummarize_MissingFile_ReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	sum, err := Summarize(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sum.Total != 0 {
		t.Errorf("expected 0 total, got %d", sum.Total)
	}
}

func TestSummarize_FiltersOldEntries(t *testing.T) {
	dir := t.TempDir()

	// Write an old entry (>7 days ago) directly.
	oldEntry := `{"verb":"old","origin":"human","ts":"` + time.Now().AddDate(0, 0, -8).UTC().Format(time.RFC3339) + `"}` + "\n"
	if err := os.WriteFile(filePath(dir), []byte(oldEntry), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Write a recent entry.
	Record(dir, "status", "")

	sum, err := Summarize(dir)
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if sum.Total != 1 {
		t.Errorf("expected 1 recent entry, got %d (old entry should be filtered)", sum.Total)
	}
	if sum.VerbCounts["status"] != 1 {
		t.Errorf("expected status=1, got %v", sum.VerbCounts)
	}
}

func TestRecord_OriginHuman(t *testing.T) {
	dir := t.TempDir()
	// Ensure neither GG_ROLE nor GG_AGENT is set so classify() returns "human".
	t.Setenv("GG_ROLE", "")
	t.Setenv("GG_AGENT", "")

	Record(dir, "status", "")

	sum, err := Summarize(dir)
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if sum.HumanCalls != 1 || sum.AgentCalls != 0 {
		t.Errorf("expected 1 human call, got human=%d agent=%d", sum.HumanCalls, sum.AgentCalls)
	}
}

func TestRecord_OriginAgent(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GG_ROLE", "developer")

	Record(dir, "record", "")

	sum, err := Summarize(dir)
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if sum.AgentCalls != 1 || sum.HumanCalls != 0 {
		t.Errorf("expected 1 agent call, got human=%d agent=%d", sum.HumanCalls, sum.AgentCalls)
	}
}

// TestRecord_OriginAgent_FromFlag — passing --from value (e.g. agent calling
// `gg tell "qa" "msg" --from architect`) classifies the call as agent-initiated
// even when GG_ROLE/GG_AGENT are unset. This catches the realistic dogfood
// case where agents follow AGENTS.md but forget to export GG_ROLE.
func TestRecord_OriginAgent_FromFlag(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GG_ROLE", "")
	t.Setenv("GG_AGENT", "")

	Record(dir, "tell", "architect")

	sum, err := Summarize(dir)
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if sum.AgentCalls != 1 || sum.HumanCalls != 0 {
		t.Errorf("expected 1 agent call (via --from), got human=%d agent=%d", sum.HumanCalls, sum.AgentCalls)
	}
}

// TestRecord_OriginAgent_GGAgentEnv — GG_AGENT env (e.g. set by Claude Code
// or GSD wrapper scripts) classifies as agent without forcing role choice.
func TestRecord_OriginAgent_GGAgentEnv(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GG_ROLE", "")
	t.Setenv("GG_AGENT", "claude-code")

	Record(dir, "search", "")

	sum, err := Summarize(dir)
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if sum.AgentCalls != 1 || sum.HumanCalls != 0 {
		t.Errorf("expected 1 agent call (via GG_AGENT), got human=%d agent=%d", sum.HumanCalls, sum.AgentCalls)
	}
}

func TestRecordCompact_AggregatesSizes(t *testing.T) {
	dir := t.TempDir()

	RecordCompact(dir, "context", "", 200, 1000, 1)
	RecordCompact(dir, "search", "", 150, 500, 1)
	Record(dir, "status", "") // non-compact — must not pollute compact totals

	sum, err := Summarize(dir)
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if sum.Total != 3 {
		t.Errorf("Total = %d, want 3", sum.Total)
	}
	if sum.CompactCalls != 2 {
		t.Errorf("CompactCalls = %d, want 2", sum.CompactCalls)
	}
	if sum.CompactBytesOut != 350 {
		t.Errorf("CompactBytesOut = %d, want 350", sum.CompactBytesOut)
	}
	if sum.CompactBytesDefault != 1500 {
		t.Errorf("CompactBytesDefault = %d, want 1500", sum.CompactBytesDefault)
	}
	// Saved = 1150, 76.6% reduction — sanity check the ratio arithmetic
	// that gg status will run on this data.
	saved := sum.CompactBytesDefault - sum.CompactBytesOut
	if saved != 1150 {
		t.Errorf("bytes saved = %d, want 1150", saved)
	}
}

func TestRecordCompact_OmittedOnNonCompact(t *testing.T) {
	dir := t.TempDir()
	Record(dir, "status", "")

	data, err := os.ReadFile(filePath(dir))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	// The omitempty JSON tags must keep non-compact entries clean — agents
	// parsing the log by hand shouldn't see noise fields on every row.
	for _, field := range []string{`"compact"`, `"bytes_out"`, `"bytes_default"`} {
		if contains := string(data); len(contains) == 0 {
			t.Fatal("empty telemetry file")
		} else if containsField(contains, field) {
			t.Errorf("non-compact entry leaked %q into JSON: %s", field, contains)
		}
	}
}

// ── RecordDupeCheck aggregation (TASK-268) ──────────────────────────────────

func TestRecordDupeCheck_CountsAllChoiceKinds(t *testing.T) {
	dir := t.TempDir()
	// Mix of choices across two "bug-report" invocations — Summarize should
	// bucket each choice into its own counter.
	RecordDupeCheck(dir, "bug-report", "", 2, 0.91, DupeChoiceForce)
	RecordDupeCheck(dir, "bug-report", "", 1, 0.86, DupeChoiceCancel)
	RecordDupeCheck(dir, "bug-report", "", 3, 0.95, DupeChoiceAutoForce)
	RecordDupeCheck(dir, "bug-report", "", 0, 0, DupeChoiceReuse) // threshold met but no matches left after filter
	Record(dir, "status", "")                                     // unrelated — must not pollute dupe totals

	sum, err := Summarize(dir)
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if sum.DupeCheckCalls != 4 {
		t.Errorf("DupeCheckCalls = %d, want 4", sum.DupeCheckCalls)
	}
	// MatchesHits counts only entries where matches_count > 0 (3 of 4).
	if sum.DupeCheckMatchesHits != 3 {
		t.Errorf("DupeCheckMatchesHits = %d, want 3 (only 3 had matches>0)", sum.DupeCheckMatchesHits)
	}
	if sum.DupeChoiceForce != 1 || sum.DupeChoiceCancel != 1 ||
		sum.DupeChoiceAutoForce != 1 || sum.DupeChoiceReuse != 1 {
		t.Errorf("choice buckets wrong: force=%d cancel=%d auto-force=%d reuse=%d",
			sum.DupeChoiceForce, sum.DupeChoiceCancel,
			sum.DupeChoiceAutoForce, sum.DupeChoiceReuse)
	}
}

func TestRecordDupeCheck_OmittedFieldsOnNonDupeEntries(t *testing.T) {
	dir := t.TempDir()
	Record(dir, "status", "")

	data, err := os.ReadFile(filePath(dir))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	// Every dupe-check field is omitempty — non-dupe-check entries must not
	// leak them into the JSONL. A grep-happy user shouldn't see "dupe_check"
	// on every row.
	for _, field := range []string{`"dupe_check"`, `"matches_count"`, `"top_score"`, `"user_choice"`} {
		if containsField(string(data), field) {
			t.Errorf("non-dupe-check entry leaked %q into JSON: %s", field, data)
		}
	}
}

func containsField(s, f string) bool {
	for i := 0; i+len(f) <= len(s); i++ {
		if s[i:i+len(f)] == f {
			return true
		}
	}
	return false
}

// TestPathResolver verifies that filePath appends telemetry.jsonl to the
// provided runtimeDir — this is the contract callers rely on when they pass
// config.RuntimeDir() instead of ggDir.
// ── RecordHydration aggregation (TASK-279) ─────────────────────────────────

func TestRecordHydration_NetSavingsPositive(t *testing.T) {
	dir := t.TempDir()
	// Compact saved 800 bytes gross; re-fetch pulled back 200 → net 600.
	RecordCompact(dir, "context", "", 200, 1000, 1)
	RecordHydration(dir, "get", "", 200)

	sum, err := Summarize(dir)
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if sum.HydrationCalls != 1 {
		t.Errorf("HydrationCalls = %d, want 1", sum.HydrationCalls)
	}
	if sum.HydrationBytesTotal != 200 {
		t.Errorf("HydrationBytesTotal = %d, want 200", sum.HydrationBytesTotal)
	}
	// gross saved = 800, hydration = 200 → net = 600
	if sum.NetSavingsBytes != 600 {
		t.Errorf("NetSavingsBytes = %d, want 600", sum.NetSavingsBytes)
	}
	if sum.NetTokensSaved != 150 {
		t.Errorf("NetTokensSaved = %d, want 150 (600/4)", sum.NetTokensSaved)
	}
}

func TestRecordHydration_NetSavingsNegative(t *testing.T) {
	dir := t.TempDir()
	// Compact saved only 100 bytes but re-fetch pulled back 300 → net −200.
	RecordCompact(dir, "context", "", 900, 1000, 1)
	RecordHydration(dir, "get", "", 300)

	sum, err := Summarize(dir)
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if sum.NetSavingsBytes != -200 {
		t.Errorf("NetSavingsBytes = %d, want -200", sum.NetSavingsBytes)
	}
}

func TestRecordHydration_OmittedOnNonHydrationEntries(t *testing.T) {
	dir := t.TempDir()
	Record(dir, "status", "")

	data, err := os.ReadFile(filePath(dir))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	for _, field := range []string{`"hydration"`, `"bytes_hydrated"`} {
		if containsField(string(data), field) {
			t.Errorf("non-hydration entry leaked %q into JSON: %s", field, data)
		}
	}
}

func TestRecordHydration_NoCompact_NetZero(t *testing.T) {
	dir := t.TempDir()
	// Hydration with no compact calls — gross saved = 0, net = -hydrated.
	RecordHydration(dir, "get", "", 500)

	sum, err := Summarize(dir)
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if sum.HydrationCalls != 1 {
		t.Errorf("HydrationCalls = %d, want 1", sum.HydrationCalls)
	}
	if sum.NetSavingsBytes != -500 {
		t.Errorf("NetSavingsBytes = %d, want -500 (no compact to offset)", sum.NetSavingsBytes)
	}
}

func TestPathResolver_UsesRuntimeDir(t *testing.T) {
	dir := t.TempDir()
	got := filePath(dir)
	want := dir + "/" + fileName
	if got != want {
		t.Errorf("filePath(%q) = %q, want %q", dir, got, want)
	}
}

func TestRecord_WritesToRuntimeDir(t *testing.T) {
	runtimeDir := t.TempDir()
	Record(runtimeDir, "status", "")

	data, err := os.ReadFile(filePath(runtimeDir))
	if err != nil {
		t.Fatalf("telemetry file not found in runtimeDir: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("telemetry file empty")
	}
}
