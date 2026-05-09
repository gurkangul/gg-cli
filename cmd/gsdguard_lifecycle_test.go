// Package cmd — tests for the role-based lifecycle gate.
// Verifies that implementation roles cannot call reviewer/verifier lifecycle
// transitions, while agent brands themselves do not determine authority.
package cmd

import (
	"strings"
	"testing"
)

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
