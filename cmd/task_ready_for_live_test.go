// Package cmd — tests for the opt-in ready_for_live + verifier-separation gate
// installed by TASK-220. These tests exercise the pure gate predicate so every
// branch is covered without a live Qdrant.
package cmd

import (
	"testing"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/store"
)

// gateOffIsAlwaysAllowed — the whole gate is opt-in. When
// require_ready_for_live is false (default) the predicate must never refuse,
// regardless of task status, verifier, or whether verifier_separation is
// toggled. This guards the back-compat invariant that 212 existing task
// closures keep working unchanged.
func TestReadyForLiveGate_OptOut_AlwaysAllows(t *testing.T) {
	cfg := &config.TasksConfig{RequireReadyForLive: false, VerifierSeparation: true}
	task := &store.Task{ID: "TASK-001", Status: "in_progress"}
	if rej := checkReadyForLiveGate(task, cfg, "", ""); rej != nil {
		t.Fatalf("opt-out path must allow the transition, got refusal: %v", rej)
	}
}

// gateOn_WrongStatus — when the gate is enabled and the task has not been
// transitioned to ready_for_live, done must be refused with ExitVerifyFailed
// and a message pointing the caller at the missing ready-for-live step.
func TestReadyForLiveGate_On_WrongStatus_Rejected(t *testing.T) {
	cfg := &config.TasksConfig{RequireReadyForLive: true}
	task := &store.Task{ID: "TASK-042", Status: "in_progress"}
	rej := checkReadyForLiveGate(task, cfg, "", "")
	if rej == nil {
		t.Fatal("expected refusal when status != ready_for_live")
		return
	}
	if rej.Code != ExitVerifyFailed {
		t.Errorf("expected ExitVerifyFailed(%d), got %d", ExitVerifyFailed, rej.Code)
	}
	if rej.Message == "" || !contains(rej.Message, "durable review handoff") || !contains(rej.Message, "ready-for-live") {
		t.Errorf("message should explain missing durable ready-for-live handoff, got %q", rej.Message)
	}
}

// gateOn_CorrectStatus_SeparationOff — with require_ready_for_live on but
// verifier_separation off, any verifier (including empty) is accepted as
// long as the task is in ready_for_live.
func TestReadyForLiveGate_On_SeparationOff_AllowsAnyVerifier(t *testing.T) {
	cfg := &config.TasksConfig{RequireReadyForLive: true, VerifierSeparation: false}
	task := &store.Task{ID: "TASK-100", Status: "ready_for_live", ReadyForLiveBy: "implementer"}
	for _, verifier := range []string{"", "implementer", "someone-else"} {
		if rej := checkReadyForLiveGate(task, cfg, verifier, ""); rej != nil {
			t.Errorf("verifier=%q must pass when separation is off, got %v", verifier, rej)
		}
	}
}

// gateOn_SeparationOn_EmptyVerifier — separation enabled but the caller did
// not pass --verifier: reject so no anonymous done closes a gated task.
func TestReadyForLiveGate_SeparationOn_EmptyVerifier_Rejected(t *testing.T) {
	cfg := &config.TasksConfig{RequireReadyForLive: true, VerifierSeparation: true}
	task := &store.Task{ID: "TASK-200", Status: "ready_for_live", ReadyForLiveBy: "implementer"}
	rej := checkReadyForLiveGate(task, cfg, "   ", "")
	if rej == nil {
		t.Fatal("expected refusal when --verifier is empty/whitespace")
		return
	}
	if rej.Code != ExitVerifyFailed {
		t.Errorf("expected ExitVerifyFailed, got %d", rej.Code)
	}
	if !contains(rej.Message, "--verifier") || !contains(rej.Message, "future reviewers") {
		t.Errorf("message should tell the caller to record verifier evidence, got %q", rej.Message)
	}
}

// gateOn_SeparationOn_SameActor — the implementer cannot verify its own work.
// This is the load-bearing invariant — it maps directly to the TASK-200→207
// class of premature-closure failures that motivated the whole gate.
func TestReadyForLiveGate_SeparationOn_SameActor_Rejected(t *testing.T) {
	cfg := &config.TasksConfig{RequireReadyForLive: true, VerifierSeparation: true}
	task := &store.Task{ID: "TASK-220", Status: "ready_for_live", ReadyForLiveBy: "claude-code"}
	rej := checkReadyForLiveGate(task, cfg, "claude-code", "")
	if rej == nil {
		t.Fatal("expected refusal when verifier matches ready_for_live_by")
		return
	}
	if rej.Code != ExitVerifyFailed {
		t.Errorf("expected ExitVerifyFailed, got %d", rej.Code)
	}
	if !contains(rej.Message, "matches the actor") || !contains(rej.Message, "independent verification") {
		t.Errorf("message should name the same-actor evidence gap, got %q", rej.Message)
	}
}

// gateOn_SeparationOn_DifferentActor — happy path: separate verifier closes
// the task. No refusal; caller proceeds to UpdateTaskStatus.
func TestReadyForLiveGate_SeparationOn_DifferentActor_Allowed(t *testing.T) {
	cfg := &config.TasksConfig{RequireReadyForLive: true, VerifierSeparation: true}
	task := &store.Task{ID: "TASK-221", Status: "ready_for_live", ReadyForLiveBy: "claude-code"}
	if rej := checkReadyForLiveGate(task, cfg, "live-verifier", ""); rej != nil {
		t.Fatalf("different verifier must be allowed, got refusal: %v", rej)
	}
}

// verifierTrimming — whitespace around --verifier must be stripped before
// comparison so callers don't bypass the gate via "claude-code " (trailing
// space would not equal "claude-code" without trim).
func TestReadyForLiveGate_VerifierTrimmed(t *testing.T) {
	cfg := &config.TasksConfig{RequireReadyForLive: true, VerifierSeparation: true}
	task := &store.Task{ID: "TASK-300", Status: "ready_for_live", ReadyForLiveBy: "claude-code"}
	if rej := checkReadyForLiveGate(task, cfg, "  claude-code  ", ""); rej == nil {
		t.Fatal("whitespace-padded same-actor must still be rejected after trim")
	}
}

// BUG-067: a single runtime cannot be both implementer and verifier. When the
// closing identity matches the one that set ready_for_live, the gate refuses
// even if the --verifier role string differs (the spoofing path).
func TestReadyForLiveGate_SameRuntimeIdentity_Rejected(t *testing.T) {
	cfg := &config.TasksConfig{RequireReadyForLive: true, VerifierSeparation: true}
	task := &store.Task{
		ID: "TASK-401", Status: "ready_for_live",
		ReadyForLiveBy:    "implementer",
		ReadyForLiveAgent: "claude-code-sess1",
	}
	// Different role string ("reviewer") but SAME runtime identity -> reject.
	if rej := checkReadyForLiveGate(task, cfg, "reviewer", "claude-code-sess1"); rej == nil {
		t.Fatal("same runtime closing its own ready-for-live must be rejected (BUG-067)")
	}
	// Different runtime identity -> allowed.
	if rej := checkReadyForLiveGate(task, cfg, "reviewer", "claude-code-sess2"); rej != nil {
		t.Fatalf("independent runtime must be allowed, got: %v", rej)
	}
}

func TestReadyForLivePlanFromArgs(t *testing.T) {
	for _, tc := range []struct {
		name     string
		args     []string
		flagPlan string
		want     string
		wantErr  bool
	}{
		{name: "positional", args: []string{"smoke live deploy"}, want: "smoke live deploy"},
		{name: "flag", flagPlan: " verify endpoint ", want: "verify endpoint"},
		{name: "missing", wantErr: true},
		{name: "both", args: []string{"one"}, flagPlan: "two", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := readyForLivePlanFromArgs(tc.args, tc.flagPlan)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("readyForLivePlanFromArgs: %v", err)
			}
			if got != tc.want {
				t.Fatalf("plan = %q, want %q", got, tc.want)
			}
		})
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
