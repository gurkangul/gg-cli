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

	RecordCompact(dir, "context", "", 200, 1000, 1, "")
	RecordCompact(dir, "search", "", 150, 500, 1, "")
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
	RecordCompact(dir, "context", "", 200, 1000, 1, "")
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
	if want := 600 / BytesPerToken; sum.NetTokensSaved != want {
		t.Errorf("NetTokensSaved = %d, want %d (600/BytesPerToken)", sum.NetTokensSaved, want)
	}
}

func TestRecordHydration_NetSavingsNegative(t *testing.T) {
	dir := t.TempDir()
	// Compact saved only 100 bytes but re-fetch pulled back 300 → net −200.
	RecordCompact(dir, "context", "", 900, 1000, 1, "")
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

func TestRecordHydration_VerbBreakdown(t *testing.T) {
	dir := t.TempDir()
	RecordHydration(dir, "get", "", 100)
	RecordHydration(dir, "get", "", 200)
	RecordHydration(dir, "decisions", "", 150)

	sum, err := Summarize(dir)
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if sum.HydrationCalls != 3 {
		t.Errorf("HydrationCalls = %d, want 3", sum.HydrationCalls)
	}
	if sum.HydrationVerbCounts["get"] != 2 {
		t.Errorf("HydrationVerbCounts[get] = %d, want 2", sum.HydrationVerbCounts["get"])
	}
	if sum.HydrationVerbCounts["decisions"] != 1 {
		t.Errorf("HydrationVerbCounts[decisions] = %d, want 1", sum.HydrationVerbCounts["decisions"])
	}
}

// TestRecordHydration_RefetchRateThreshold verifies that when HydrationCalls
// exceeds 50% of CompactCalls the signal is reflected in the summary fields
// that the gg telemetry summary warning depends on.
func TestRecordHydration_RefetchRateThreshold(t *testing.T) {
	dir := t.TempDir()
	// 2 compact calls, 2 hydration calls → 100% re-fetch rate (>50%).
	RecordCompact(dir, "get", "", 200, 1000, 2, "")
	RecordCompact(dir, "get", "", 200, 1000, 2, "")
	RecordHydration(dir, "get", "", 500)
	RecordHydration(dir, "get", "", 500)

	sum, err := Summarize(dir)
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if sum.CompactCalls != 2 {
		t.Errorf("CompactCalls = %d, want 2", sum.CompactCalls)
	}
	if sum.HydrationCalls != 2 {
		t.Errorf("HydrationCalls = %d, want 2", sum.HydrationCalls)
	}
	// refetchPct = 100*2/2 = 100 > 50
	refetchPct := 0
	if sum.CompactCalls > 0 {
		refetchPct = 100 * sum.HydrationCalls / sum.CompactCalls
	}
	if refetchPct <= 50 {
		t.Errorf("refetchPct = %d, want >50", refetchPct)
	}
	// gross saved = (1000-200)*2 = 1600; hydration = 1000; net = 600
	if sum.NetSavingsBytes != 600 {
		t.Errorf("NetSavingsBytes = %d, want 600", sum.NetSavingsBytes)
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

// ── RecordMissingHandler aggregation (TASK-283) ────────────────────────────────

func TestRecordMissingHandler_CountsAndBucketsVerbs(t *testing.T) {
	dir := t.TempDir()
	RecordMissingHandler(dir, "gaps", "")
	RecordMissingHandler(dir, "gaps", "")
	RecordMissingHandler(dir, "file-size", "")
	Record(dir, "status", "") // unrelated — must not pollute missing-handler totals

	sum, err := Summarize(dir)
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if sum.Total != 4 {
		t.Errorf("Total = %d, want 4", sum.Total)
	}
	if sum.MissingHandlerCalls != 3 {
		t.Errorf("MissingHandlerCalls = %d, want 3", sum.MissingHandlerCalls)
	}
	if sum.MissingHandlerVerbCounts["gaps"] != 2 {
		t.Errorf("MissingHandlerVerbCounts[gaps] = %d, want 2", sum.MissingHandlerVerbCounts["gaps"])
	}
	if sum.MissingHandlerVerbCounts["file-size"] != 1 {
		t.Errorf("MissingHandlerVerbCounts[file-size] = %d, want 1", sum.MissingHandlerVerbCounts["file-size"])
	}
	// Non-missing-handler entries must not leak into the counter.
	if sum.MissingHandlerVerbCounts["status"] != 0 {
		t.Errorf("status should not appear in MissingHandlerVerbCounts, got %d", sum.MissingHandlerVerbCounts["status"])
	}
}

func TestRecordMissingHandler_OmittedOnNonMissingEntries(t *testing.T) {
	dir := t.TempDir()
	Record(dir, "status", "")

	data, err := os.ReadFile(filePath(dir))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	// The omitempty tag must keep non-missing-handler entries clean.
	if containsField(string(data), `"missing_handler"`) {
		t.Errorf("non-missing-handler entry leaked %q into JSON: %s", `"missing_handler"`, data)
	}
}

func TestRecordMissingHandler_ZeroWhenNoneRecorded(t *testing.T) {
	dir := t.TempDir()
	RecordCompact(dir, "context", "", 200, 1000, 1, "")
	Record(dir, "status", "")

	sum, err := Summarize(dir)
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if sum.MissingHandlerCalls != 0 {
		t.Errorf("MissingHandlerCalls = %d, want 0 when none recorded", sum.MissingHandlerCalls)
	}
	if len(sum.MissingHandlerVerbCounts) != 0 {
		t.Errorf("MissingHandlerVerbCounts = %v, want empty map", sum.MissingHandlerVerbCounts)
	}
}

func TestRecordMissingHandler_ClassifiesOriginCorrectly(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GG_ROLE", "developer")
	RecordMissingHandler(dir, "gaps", "")

	sum, err := Summarize(dir)
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if sum.AgentCalls != 1 {
		t.Errorf("AgentCalls = %d, want 1 (origin should be agent when GG_ROLE set)", sum.AgentCalls)
	}
}

// TestRecordMissingHandler_OldEntriesExcluded verifies that MissingHandler
// entries older than 7 days are not counted by Summarize, consistent with the
// behaviour of every other entry type.
func TestRecordMissingHandler_OldEntriesExcluded(t *testing.T) {
	dir := t.TempDir()

	// Write a stale missing-handler entry (>7 days ago) directly.
	oldEntry := `{"verb":"gaps","origin":"agent","ts":"` +
		time.Now().AddDate(0, 0, -8).UTC().Format(time.RFC3339) +
		`","missing_handler":true}` + "\n"
	if err := os.WriteFile(filePath(dir), []byte(oldEntry), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Write a recent missing-handler entry.
	RecordMissingHandler(dir, "gaps", "")

	sum, err := Summarize(dir)
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if sum.MissingHandlerCalls != 1 {
		t.Errorf("MissingHandlerCalls = %d, want 1 (old entry should be excluded)", sum.MissingHandlerCalls)
	}
	if sum.MissingHandlerVerbCounts["gaps"] != 1 {
		t.Errorf("MissingHandlerVerbCounts[gaps] = %d, want 1", sum.MissingHandlerVerbCounts["gaps"])
	}
}

// TestRecordMissingHandler_AgentEnvTriggers verifies that an agent environment
// variable (GG_ROLE or GG_AGENT) causes RecordMissingHandler to classify the
// entry as agent-initiated, and that the entry appears in Summarize under
// MissingHandlerVerbCounts for the given verb.
func TestRecordMissingHandler_AgentEnvTriggers(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GG_ROLE", "developer")
	t.Setenv("GG_AGENT", "")

	RecordMissingHandler(dir, "file-size", "")

	sum, err := Summarize(dir)
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if sum.MissingHandlerCalls != 1 {
		t.Errorf("MissingHandlerCalls = %d, want 1", sum.MissingHandlerCalls)
	}
	if sum.MissingHandlerVerbCounts["file-size"] != 1 {
		t.Errorf("MissingHandlerVerbCounts[file-size] = %d, want 1", sum.MissingHandlerVerbCounts["file-size"])
	}
	if sum.AgentCalls != 1 {
		t.Errorf("AgentCalls = %d, want 1 (GG_ROLE set → agent origin)", sum.AgentCalls)
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

// ── GlyphOverheadBytesOf (TASK-281) ───────────────────────────────────────────

func TestGlyphOverheadBytesOf_PureASCII(t *testing.T) {
	// ASCII-only strings have zero overhead — every rune is 1 byte.
	for _, s := range []string{"", "hello", "T ○ TASK-001", "->", "done"} {
		// ASCII characters are <128, so ○ would not appear here — skip that one.
		_ = s
	}
	if got := GlyphOverheadBytesOf("hello world"); got != 0 {
		t.Errorf("GlyphOverheadBytesOf(\"hello world\") = %d, want 0", got)
	}
	if got := GlyphOverheadBytesOf(""); got != 0 {
		t.Errorf("GlyphOverheadBytesOf(\"\") = %d, want 0", got)
	}
}

func TestGlyphOverheadBytesOf_SingleGlyph(t *testing.T) {
	// ✓ (U+2713) encodes as 3 UTF-8 bytes → overhead = 3-1 = 2.
	if got := GlyphOverheadBytesOf("✓"); got != 2 {
		t.Errorf("GlyphOverheadBytesOf(\"✓\") = %d, want 2", got)
	}
	// → (U+2192) also 3 bytes → overhead = 2.
	if got := GlyphOverheadBytesOf("→"); got != 2 {
		t.Errorf("GlyphOverheadBytesOf(\"→\") = %d, want 2", got)
	}
	// ○ (U+25CB) also 3 bytes → overhead = 2.
	if got := GlyphOverheadBytesOf("○"); got != 2 {
		t.Errorf("GlyphOverheadBytesOf(\"○\") = %d, want 2", got)
	}
}

func TestGlyphOverheadBytesOf_MixedString(t *testing.T) {
	// "T ✓ TASK-001  done (high)" — one glyph ✓ (overhead 2) plus ASCII.
	s := "T ✓ TASK-001  done (high)"
	if got := GlyphOverheadBytesOf(s); got != 2 {
		t.Errorf("GlyphOverheadBytesOf(%q) = %d, want 2", s, got)
	}
}

func TestGlyphOverheadBytesOf_MultipleGlyphs(t *testing.T) {
	// "✓ → ○" — three 3-byte glyphs, each overhead 2 → total 6.
	s := "✓ → ○"
	if got := GlyphOverheadBytesOf(s); got != 6 {
		t.Errorf("GlyphOverheadBytesOf(%q) = %d, want 6", s, got)
	}
}

func TestGlyphOverheadBytesOf_StatusLine(t *testing.T) {
	// Realistic compact status output line from gg status.
	// "○ Pending: 8  → In Progress: 0  ⚠ Blocked: 0  ✓ Done: 280"
	// Glyphs: ○(2) →(2) ⚠(2) ✓(2) = 8 bytes overhead.
	s := "○ Pending: 8  → In Progress: 0  ⚠ Blocked: 0  ✓ Done: 280"
	if got := GlyphOverheadBytesOf(s); got != 8 {
		t.Errorf("GlyphOverheadBytesOf(status line) = %d, want 8", got)
	}
}

func TestRecordCompact_GlyphOverheadAggregated(t *testing.T) {
	dir := t.TempDir()
	// Two compact calls: first with a glyph string, second with ASCII only.
	// ✓ has overhead 2; total across both calls = 2.
	RecordCompact(dir, "context", "", 100, 500, 1, "T ✓ TASK-001")
	RecordCompact(dir, "search", "", 50, 200, 1, "no glyphs here")

	sum, err := Summarize(dir)
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if sum.GlyphByteOverhead != 2 {
		t.Errorf("GlyphByteOverhead = %d, want 2", sum.GlyphByteOverhead)
	}
	// 2 / BytesPerToken = 0 (integer division — overhead too small for a full token).
	if want := 2 / BytesPerToken; sum.GlyphTokenOverhead != want {
		t.Errorf("GlyphTokenOverhead = %d, want %d (2/BytesPerToken)", sum.GlyphTokenOverhead, want)
	}
}

func TestRecordCompact_GlyphOverheadLargeInput(t *testing.T) {
	dir := t.TempDir()
	// 100 copies of "✓" → overhead = 100 * 2 = 200 bytes → 200/BytesPerToken tokens.
	s := ""
	for i := 0; i < 100; i++ {
		s += "✓"
	}
	RecordCompact(dir, "context", "", len(s), len(s)+200, 1, s)

	sum, err := Summarize(dir)
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if sum.GlyphByteOverhead != 200 {
		t.Errorf("GlyphByteOverhead = %d, want 200", sum.GlyphByteOverhead)
	}
	if want := 200 / BytesPerToken; sum.GlyphTokenOverhead != want {
		t.Errorf("GlyphTokenOverhead = %d, want %d (200/BytesPerToken)", sum.GlyphTokenOverhead, want)
	}
}

func TestRecordCompact_GlyphOverheadOmittedOnNonCompact(t *testing.T) {
	dir := t.TempDir()
	Record(dir, "status", "")

	data, err := os.ReadFile(filePath(dir))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if containsField(string(data), `"glyph_overhead_bytes"`) {
		t.Errorf("non-compact entry must not contain glyph_overhead_bytes: %s", data)
	}
}

func TestSummarize_CalibrationFactor(t *testing.T) {
	dir := t.TempDir()
	Record(dir, "status", "")

	sum, err := Summarize(dir)
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	// CalibrationFactor must equal CorpusCalibration.Rounded, which is > 0 as
	// long as the compact corpus golden file is non-empty.
	if sum.CalibrationFactor != CorpusCalibration.Rounded {
		t.Errorf("CalibrationFactor = %d, want %d (CorpusCalibration.Rounded)",
			sum.CalibrationFactor, CorpusCalibration.Rounded)
	}
	if sum.CalibrationFactor <= 0 {
		t.Errorf("CalibrationFactor = %d, want > 0 (corpus must produce a non-zero ratio)",
			sum.CalibrationFactor)
	}
	// CompactTokensSaved must still use BytesPerToken, not CalibrationFactor.
	// Verify by recording a compact entry and checking the math.
	dir2 := t.TempDir()
	RecordCompact(dir2, "context", "", 100, 200, 0, "")
	sum2, err := Summarize(dir2)
	if err != nil {
		t.Fatalf("Summarize dir2: %v", err)
	}
	if want := 100 / BytesPerToken; sum2.CompactTokensSaved != want {
		t.Errorf("CompactTokensSaved = %d, want %d (100/BytesPerToken=%d, not CalibrationFactor=%d)",
			sum2.CompactTokensSaved, want, BytesPerToken, sum2.CalibrationFactor)
	}
}
