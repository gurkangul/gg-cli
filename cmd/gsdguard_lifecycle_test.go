// Package cmd — tests for the role-based lifecycle gate.
// Verifies that implementation roles cannot call reviewer/verifier lifecycle
// transitions, while agent brands themselves do not determine authority.
package cmd

import (
	"errors"
	"strings"
	"testing"
)

// parseGuardToolName tests (TASK-498) — the stdin/JSON parse path used by the
// PreToolUse guard. Previously io.ReadAll and json.Unmarshal errors were
// swallowed, so unreadable stdin or malformed JSON silently yielded an empty
// ToolName and the guard failed OPEN (allowed). These tests pin the fail-safe
// contract: read errors and malformed non-empty JSON are surfaced (caller
// blocks), valid input parses, and legitimately-empty input is a non-error
// passthrough.

func TestParseGuardToolName_ValidPayload(t *testing.T) {
	name, err := parseGuardToolName(strings.NewReader(`{"tool_name":"mcp__gsd-workflow__gsd_plan_milestone"}`))
	if err != nil {
		t.Fatalf("valid payload must not error, got %v", err)
	}
	if name != "mcp__gsd-workflow__gsd_plan_milestone" {
		t.Errorf("unexpected tool name: %q", name)
	}
}

func TestParseGuardToolName_ValidAllowedTool(t *testing.T) {
	name, err := parseGuardToolName(strings.NewReader(`{"tool_name":"Bash"}`))
	if err != nil {
		t.Fatalf("valid payload must not error, got %v", err)
	}
	if name != "Bash" {
		t.Errorf("unexpected tool name: %q", name)
	}
}

func TestParseGuardToolName_MalformedJSONIsError(t *testing.T) {
	_, err := parseGuardToolName(strings.NewReader(`{"tool_name": "Bash"`)) // truncated
	if err == nil {
		t.Fatal("malformed non-empty JSON must surface an error so the guard fails safe (blocks), not silently allow")
	}
	if !strings.Contains(err.Error(), "parse PreToolUse JSON") {
		t.Errorf("error should identify the parse failure, got %q", err.Error())
	}
}

func TestParseGuardToolName_GarbageBytesIsError(t *testing.T) {
	if _, err := parseGuardToolName(strings.NewReader("not json at all")); err == nil {
		t.Fatal("non-JSON non-empty body must surface an error, not be treated as empty ToolName")
	}
}

func TestParseGuardToolName_EmptyStdinIsPassthrough(t *testing.T) {
	// Legitimately-empty payload: nothing to block, so this is a non-error
	// passthrough (empty tool name → caller's forbidden loop won't match).
	for _, in := range []string{"", "   ", "\n\t"} {
		name, err := parseGuardToolName(strings.NewReader(in))
		if err != nil {
			t.Errorf("empty/whitespace input %q must not error, got %v", in, err)
		}
		if name != "" {
			t.Errorf("empty input %q must yield empty tool name, got %q", in, name)
		}
	}
}

// errReader always fails, simulating an unreadable stdin.
type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("stdin boom") }

func TestParseGuardToolName_ReadErrorIsError(t *testing.T) {
	_, err := parseGuardToolName(errReader{})
	if err == nil {
		t.Fatal("unreadable stdin must surface an error so the guard fails safe (blocks), not proceed with empty input")
	}
	if !strings.Contains(err.Error(), "read stdin") {
		t.Errorf("error should identify the read failure, got %q", err.Error())
	}
}

// isLifecycleBlockedRole tests — pure predicate, no I/O or env reads.

func TestIsLifecycleBlockedRole_ImplementationRolesBlocked(t *testing.T) {
	for _, role := range []string{"developer", "DEVELOPER", "worker", "implementer"} {
		if !isLifecycleBlockedRole(role) {
			t.Errorf("expected %q to be blocked", role)
		}
	}
}

func TestIsLifecycleBlockedRole_NonImplementationRolesAllowed(t *testing.T) {
	for _, role := range []string{
		"master",
		"reviewer",
		"verifier",
		"architect",
		"",
	} {
		if isLifecycleBlockedRole(role) {
			t.Errorf("expected %q to be allowed", role)
		}
	}
}

// checkAgentLifecycleGate tests — exercises the env-dependent function via
// t.Setenv so each case is hermetic.

func TestAgentLifecycleGate_DeveloperRoleBlockedForDone(t *testing.T) {
	t.Setenv("GG_AGENT", "codex")
	t.Setenv("GG_ROLE", "developer")
	rej := checkAgentLifecycleGate("done")
	if rej == nil {
		t.Fatal("expected gate to block GG_ROLE=developer")
		return
	}
	if rej.Code != ExitVerifyFailed {
		t.Errorf("expected ExitVerifyFailed(%d), got %d", ExitVerifyFailed, rej.Code)
	}
	if !strings.Contains(rej.Message, "developer") {
		t.Errorf("message should name the blocked role, got %q", rej.Message)
	}
	if !strings.Contains(rej.Message, "done") {
		t.Errorf("message should name the verb, got %q", rej.Message)
	}
	if !strings.Contains(rej.Message, "independent review evidence") || !strings.Contains(rej.Message, "not your native workflow") {
		t.Errorf("message should explain the missing evidence, not ritual compliance, got %q", rej.Message)
	}
}

func TestAgentLifecycleGate_AgentBrandAloneDoesNotBlockReadyForLive(t *testing.T) {
	t.Setenv("GG_AGENT", "claude-sonnet-4-6")
	t.Setenv("GG_ROLE", "developer")
	rej := checkAgentLifecycleGate("ready-for-live")
	if rej != nil {
		t.Fatalf("ready-for-live should be allowed for implementation roles, got %v", rej)
	}
}

func TestAgentLifecycleGate_ReviewerRoleAllowedRegardlessOfAgent(t *testing.T) {
	t.Setenv("GG_AGENT", "codex")
	t.Setenv("GG_ROLE", "master")
	if rej := checkAgentLifecycleGate("done"); rej != nil {
		t.Fatalf("master role must be allowed, got refusal: %v", rej)
	}
}

func TestAgentLifecycleGate_EmptyAgent_Allowed(t *testing.T) {
	t.Setenv("GG_AGENT", "")
	t.Setenv("GG_ROLE", "")
	if rej := checkAgentLifecycleGate("done"); rej != nil {
		t.Fatalf("empty GG_AGENT must be allowed (untagged session), got refusal: %v", rej)
	}
}

func TestAgentLifecycleGate_AgentNameGSDWithoutWorkerRoleAllowed(t *testing.T) {
	t.Setenv("GG_AGENT", "gsd")
	t.Setenv("GG_ROLE", "master")
	rej := checkAgentLifecycleGate("done")
	if rej != nil {
		t.Fatalf("agent/runtime brand must not decide authority, got %v", rej)
	}
}

func TestAgentLifecycleGate_GSDAsDeveloperRoleBlocked(t *testing.T) {
	t.Setenv("GG_AGENT", "GSD")
	t.Setenv("GG_ROLE", "developer")
	if rej := checkAgentLifecycleGate("done"); rej == nil {
		t.Fatal("developer role must be blocked even when runtime is GSD")
	}
}

// review verb gate tests (TASK-288)

func TestAgentLifecycleGate_ReviewDeveloperRoleBlocked(t *testing.T) {
	t.Setenv("GG_AGENT", "gsd")
	t.Setenv("GG_ROLE", "developer")
	rej := checkAgentLifecycleGate("review")
	if rej == nil {
		t.Fatal("expected gate to block GG_ROLE=developer for 'review' verb")
		return
	}
	if rej.Code != ExitVerifyFailed {
		t.Errorf("expected ExitVerifyFailed(%d), got %d", ExitVerifyFailed, rej.Code)
	}
	if !strings.Contains(rej.Message, "review") {
		t.Errorf("message should name the verb 'review', got %q", rej.Message)
	}
}

func TestAgentLifecycleGate_ReviewReviewerRoleAllowed(t *testing.T) {
	t.Setenv("GG_AGENT", "cursor")
	t.Setenv("GG_ROLE", "reviewer")
	if rej := checkAgentLifecycleGate("review"); rej != nil {
		t.Fatalf("reviewer role must be allowed to call 'review', got refusal: %v", rej)
	}
}
