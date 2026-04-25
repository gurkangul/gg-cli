package enforcement

import "testing"

// TestCheckBypassRationale_AC3a: bypass without GG_BYPASS_RATIONALE is rejected.
func TestCheckBypassRationale_NoRationale_Rejected(t *testing.T) {
	t.Setenv(BypassRationaleEnvVar, "")
	_, err := CheckBypassRationale("TASK-317")
	if err == nil {
		t.Fatal("expected error for empty rationale, got nil")
	}
}

// TestCheckBypassRationale_AC3a_NoTaskID: even with no task, empty rationale rejected.
func TestCheckBypassRationale_NoRationale_NoTask_Rejected(t *testing.T) {
	t.Setenv(BypassRationaleEnvVar, "")
	_, err := CheckBypassRationale("")
	if err == nil {
		t.Fatal("expected error for empty rationale (no-task gate), got nil")
	}
}

// TestCheckBypassRationale_AC3b: bypass with valid rationale accepted and fields populated.
func TestCheckBypassRationale_WithRationale_Accepted(t *testing.T) {
	t.Setenv(BypassRationaleEnvVar, "TASK-317: AC attestation is an analytical task, no code output")
	res, err := CheckBypassRationale("TASK-317")
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if res.Rationale != "TASK-317: AC attestation is an analytical task, no code output" {
		t.Errorf("Rationale mismatch: %q", res.Rationale)
	}
	if res.RationaleTaskID != "TASK-317" {
		t.Errorf("RationaleTaskID: got %q, want TASK-317", res.RationaleTaskID)
	}
}

// TestCheckBypassRationale_AC3b_NoTaskPrefix: rationale without task prefix is also accepted.
func TestCheckBypassRationale_NoTaskPrefix_Accepted(t *testing.T) {
	t.Setenv(BypassRationaleEnvVar, "emergency hotfix — deploy blocked by flaky integration test")
	res, err := CheckBypassRationale("TASK-001")
	if err != nil {
		t.Fatalf("expected success for rationale without task prefix: %v", err)
	}
	if res.RationaleTaskID != "" {
		t.Errorf("expected empty RationaleTaskID for no-prefix rationale, got %q", res.RationaleTaskID)
	}
}

// TestCheckBypassRationale_AC3c: rationale referencing wrong task is rejected.
func TestCheckBypassRationale_WrongTask_Rejected(t *testing.T) {
	t.Setenv(BypassRationaleEnvVar, "TASK-999: some other task rationale")
	_, err := CheckBypassRationale("TASK-317")
	if err == nil {
		t.Fatal("expected error when rationale task mismatches gate task")
	}
}

// TestCheckBypassRationale_AC3c_SameTask_Accepted: rationale with matching task prefix passes.
func TestCheckBypassRationale_SameTask_Accepted(t *testing.T) {
	t.Setenv(BypassRationaleEnvVar, "TASK-042: gate-is-analytics-task, no code diff to attest")
	res, err := CheckBypassRationale("TASK-042")
	if err != nil {
		t.Fatalf("matching task prefix should pass: %v", err)
	}
	if res.RationaleTaskID != "TASK-042" {
		t.Errorf("RationaleTaskID: got %q, want TASK-042", res.RationaleTaskID)
	}
}

// TestCheckBypassRationale_EmptyTaskID_NoMismatchCheck: when gate has no task (e.g. gsd-guard),
// any non-empty rationale passes regardless of task prefix.
func TestCheckBypassRationale_NoGateTask_AnyRationaleAccepted(t *testing.T) {
	t.Setenv(BypassRationaleEnvVar, "TASK-999: planning call, gsd-guard not applicable here")
	_, err := CheckBypassRationale("")
	if err != nil {
		t.Fatalf("no task gate should accept any rationale: %v", err)
	}
}

// TestExtractTaskIDFromRationale covers the parser for task prefix extraction.
func TestExtractTaskIDFromRationale(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"TASK-317: some reason", "TASK-317"},
		{"task-42: lowercase works", "TASK-42"},
		{"TASK-1: single digit", "TASK-1"},
		{"TASK-1234567: big number", "TASK-1234567"},
		{"no prefix here", ""},
		{"TASK- no digits", ""},
		{"", ""},
		{"completely unrelated", ""},
	}
	for _, c := range cases {
		got := extractTaskIDFromRationale(c.in)
		if got != c.want {
			t.Errorf("extractTaskIDFromRationale(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
