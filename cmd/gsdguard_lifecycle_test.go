// Package cmd — tests for the agent-lifecycle gate added by TASK-272.
// Verifies that GSD / Sonnet agents cannot call gg task done or
// gg task ready-for-live, while Opus / untagged sessions pass through.
package cmd

import (
	"strings"
	"testing"
)

// isLifecycleBlockedAgent tests — pure predicate, no I/O or env reads.

func TestIsLifecycleBlockedAgent_GSD_Blocked(t *testing.T) {
	for _, agent := range []string{"gsd", "GSD", "GsD", "gsd-workflow"} {
		if !isLifecycleBlockedAgent(agent) {
			t.Errorf("expected %q to be blocked", agent)
		}
	}
}

func TestIsLifecycleBlockedAgent_Sonnet_Blocked(t *testing.T) {
	for _, agent := range []string{
		"claude-sonnet-4-6",
		"claude-sonnet-4-5",
		"CLAUDE-SONNET-4-6",
		"claude-sonnet-",
	} {
		if !isLifecycleBlockedAgent(agent) {
			t.Errorf("expected %q to be blocked", agent)
		}
	}
}

func TestIsLifecycleBlockedAgent_Opus_Allowed(t *testing.T) {
	for _, agent := range []string{
		"claude-code",
		"claude-opus-4-7",
		"claude-opus-4-6",
		"opus",
		"developer",
		"architect",
		"",
	} {
		if isLifecycleBlockedAgent(agent) {
			t.Errorf("expected %q to be allowed", agent)
		}
	}
}

// checkAgentLifecycleGate tests — exercises the env-dependent function via
// t.Setenv so each case is hermetic.

func TestAgentLifecycleGate_GSD_Blocked(t *testing.T) {
	t.Setenv("GG_AGENT", "gsd")
	t.Setenv("GG_ROLE", "")
	rej := checkAgentLifecycleGate("done")
	if rej == nil {
		t.Fatal("expected gate to block GG_AGENT=gsd")
	}
	if rej.Code != ExitVerifyFailed {
		t.Errorf("expected ExitVerifyFailed(%d), got %d", ExitVerifyFailed, rej.Code)
	}
	if !strings.Contains(rej.Message, "gsd") {
		t.Errorf("message should name the blocked agent, got %q", rej.Message)
	}
	if !strings.Contains(rej.Message, "done") {
		t.Errorf("message should name the verb, got %q", rej.Message)
	}
}

func TestAgentLifecycleGate_Sonnet_Blocked(t *testing.T) {
	t.Setenv("GG_AGENT", "claude-sonnet-4-6")
	t.Setenv("GG_ROLE", "")
	rej := checkAgentLifecycleGate("ready-for-live")
	if rej == nil {
		t.Fatal("expected gate to block GG_AGENT=claude-sonnet-4-6")
	}
	if rej.Code != ExitVerifyFailed {
		t.Errorf("expected ExitVerifyFailed(%d), got %d", ExitVerifyFailed, rej.Code)
	}
}

func TestAgentLifecycleGate_OpusAllowed(t *testing.T) {
	t.Setenv("GG_AGENT", "claude-code")
	t.Setenv("GG_ROLE", "")
	if rej := checkAgentLifecycleGate("done"); rej != nil {
		t.Fatalf("claude-code must be allowed, got refusal: %v", rej)
	}
}

func TestAgentLifecycleGate_EmptyAgent_Allowed(t *testing.T) {
	t.Setenv("GG_AGENT", "")
	t.Setenv("GG_ROLE", "")
	if rej := checkAgentLifecycleGate("done"); rej != nil {
		t.Fatalf("empty GG_AGENT must be allowed (untagged session), got refusal: %v", rej)
	}
}

func TestAgentLifecycleGate_RoleFallback_GSD_Blocked(t *testing.T) {
	// When GG_AGENT is unset but GG_ROLE=gsd, the gate must still block.
	t.Setenv("GG_AGENT", "")
	t.Setenv("GG_ROLE", "gsd")
	rej := checkAgentLifecycleGate("done")
	if rej == nil {
		t.Fatal("expected gate to block when GG_ROLE=gsd and GG_AGENT is empty")
	}
}

func TestAgentLifecycleGate_GSDPrefix_CaseInsensitive(t *testing.T) {
	t.Setenv("GG_AGENT", "GSD")
	t.Setenv("GG_ROLE", "")
	if rej := checkAgentLifecycleGate("done"); rej == nil {
		t.Fatal("GSD (uppercase) must be blocked — matching is case-insensitive")
	}
}
