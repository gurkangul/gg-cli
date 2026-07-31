#!/bin/sh
set -eu

# Repro for BUG-106: every durable-write verb stamped its author from $GG_ROLE
# alone, so an agent runtime that never exported a role wrote author="" —
# silently, and indistinguishably from a record whose author simply was not
# printed by that view.
#
# The sharp edge is that gg had already ACCEPTED that session's identity:
# requireAgentIdentity() refuses any state-changing write unless GG_ROLE or
# GG_AGENT is set, "so shared evidence and handoffs are attributable". gg
# verified the identity at the door and then stamped the record from a different
# variable. 436 records in this repo's own ledger are anonymous despite passing
# that check.
#
# The fix routes every durable stamp through one ladder —
#   --from -> $GG_ROLE -> identity.Agent() -> ""
# — via resolveAuthor / resolveAuthorEnv, renders an unresolvable author as
# [anonymous] instead of dropping the field, and adds opt-in GG_REQUIRE_AUTHOR.
#
# This repro guards all three properties hermetically, plus the property the
# first pass missed: the lifecycle broadcast must carry the SAME identity the
# command stamps on the task, or `gg task start` reports one actor and its own
# broadcast reports another. At the broken ref the ladder stops at GG_ROLE, so
# the GG_AGENT case resolves to "" and the test fails.

cat > cmd/bug106_repro_test.go <<'GO'
package cmd

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/gurkangul/gg-cli/internal/store"
)

// clearIdentityEnv removes every signal the ladder consults so each case states
// exactly which rung it is exercising.
func clearIdentityEnvBUG106(t *testing.T) {
	t.Helper()
	for _, k := range []string{"GG_ROLE", "GG_AGENT", "CLAUDE_CODE_SESSION_ID", "CLAUDE_SESSION_ID", "GG_REQUIRE_AUTHOR"} {
		t.Setenv(k, "")
	}
}

func TestBUG106AuthorLadder(t *testing.T) {
	// An agent runtime with GG_AGENT but no exported role: the case that
	// produced every anonymous record. Pre-fix this resolved to "".
	clearIdentityEnvBUG106(t)
	t.Setenv("GG_AGENT", "codex")
	if got := resolveAuthor(statusCmd); got != "codex" {
		t.Fatalf("BUG-106: GG_AGENT-only runtime resolved to %q — a durable write would land anonymous", got)
	}

	// The per-tab sharpening (BUG-084) must reach the author field too, or two
	// concurrent tabs collapse into one provenance.
	clearIdentityEnvBUG106(t)
	t.Setenv("GG_AGENT", "claude-code")
	t.Setenv("CLAUDE_CODE_SESSION_ID", "deadbeef99887766")
	if got := resolveAuthor(statusCmd); got != "claude-code-deadbeef" {
		t.Fatalf("BUG-106: per-tab identity did not reach the author field, got %q", got)
	}

	// An exported role is the provenance the operator MEANS; the agent id is
	// only the runtime that happened to execute the command.
	clearIdentityEnvBUG106(t)
	t.Setenv("GG_AGENT", "claude-code")
	t.Setenv("GG_ROLE", "master")
	if got := resolveAuthor(statusCmd); got != "master" {
		t.Fatalf("BUG-106: GG_ROLE must outrank the agent id, got %q", got)
	}

	// No signal at all — a bare human shell. Still empty, and that is correct:
	// gg must not invent a name.
	clearIdentityEnvBUG106(t)
	if got := resolveAuthor(statusCmd); got != "" {
		t.Fatalf("BUG-106: expected no author with no identity signal, got %q", got)
	}
}

func TestBUG106AnonymityIsVisible(t *testing.T) {
	// Pre-fix an empty author was DROPPED by every renderer ("if Author != \"\""),
	// so an anonymous record looked identical to one whose author that view
	// simply did not print. gg already marks its other missing provenance signal
	// ([unverified] for absent evidence); author was the lone exception.
	if got := authorLabel(""); got != anonymousAuthorLabel {
		t.Fatalf("BUG-106: empty author rendered %q, expected %s", got, anonymousAuthorLabel)
	}

	var buf bytes.Buffer
	renderTaskListDefault(&buf, []store.Task{
		{ID: "TASK-001", Title: "no author", Status: "todo", Priority: "high"},
		{ID: "TASK-002", Title: "has author", Status: "todo", Priority: "high", Author: "master"},
	})
	out := buf.String()
	if !strings.Contains(out, "("+anonymousAuthorLabel+")") {
		t.Fatalf("BUG-106: task list hid the missing author:\n%s", out)
	}
	if !strings.Contains(out, "(master)") {
		t.Fatalf("BUG-106: task list dropped a real author:\n%s", out)
	}

	buf.Reset()
	renderTaskGetDefault(&buf, &store.Task{ID: "TASK-003", Title: "no author", Status: "todo"})
	if !strings.Contains(buf.String(), "By: "+anonymousAuthorLabel) {
		t.Fatalf("BUG-106: task get hid the missing author:\n%s", buf.String())
	}
}

func TestBUG106StrictModeIsOptIn(t *testing.T) {
	clearIdentityEnvBUG106(t)

	// Default OFF: a human in a bare shell, and CI, must never be blocked by a
	// convention they did not adopt.
	if _, err := requireAuthor(statusCmd); err != nil {
		t.Fatalf("BUG-106: strict mode must be opt-in, got %v", err)
	}

	t.Setenv("GG_REQUIRE_AUTHOR", "1")
	if _, err := requireAuthor(statusCmd); err == nil {
		t.Fatal("BUG-106: GG_REQUIRE_AUTHOR=1 must reject an unattributable write")
	}

	// Satisfied by any rung of the ladder, not just --from.
	t.Setenv("GG_AGENT", "codex")
	if got, err := requireAuthor(statusCmd); err != nil || got != "codex" {
		t.Fatalf("BUG-106: strict mode should accept the agent identity, got %q err=%v", got, err)
	}
}

func TestBUG106BroadcastMatchesTaskOwner(t *testing.T) {
	// The review pass caught this one: notifyTaskLifecycle read raw GG_AGENT, so
	// `gg task start` printed "started by claude-code-<sid>" while the broadcast
	// it emitted in the same call said plain "claude-code" — one action, two
	// identities, which is the collapse BUG-084 was filed to fix.
	clearIdentityEnvBUG106(t)
	t.Setenv("GG_NO_AUTO_NOTIFY", "")
	t.Setenv("GG_AGENT", "claude-code")
	t.Setenv("CLAUDE_CODE_SESSION_ID", "deadbeef99887766")

	sender := &mockMessageSender{}
	notifyTaskLifecycle(context.Background(), sender, "TASK-001", "started", "x")
	if len(sender.msgs) != 1 {
		t.Fatalf("expected 1 broadcast, got %d", len(sender.msgs))
	}
	if got, want := sender.msgs[0].FromRole, resolveTaskOwner(""); got != want {
		t.Fatalf("BUG-106: broadcast identity %q != task owner identity %q", got, want)
	}
}
GO

trap 'rm -f cmd/bug106_repro_test.go' EXIT

go test ./cmd -run 'TestBUG106' -count=1
