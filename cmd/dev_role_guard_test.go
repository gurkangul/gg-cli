package cmd

import (
	"testing"
)

func TestDevRoleGuardPredicate_BlocksSamePaneDeveloperPickup(t *testing.T) {
	cases := []string{
		`export GG_AGENT="claude-code" GG_ROLE="developer"; gg task create "Fix thing" --requester developer`,
		`GG_ROLE=developer gg task start TASK-123`,
		`gg tell all "TASK-123 picked up" --from developer --audience agents`,
		`GG_ROLE='developer' gg task ready-for-live TASK-123 "plan" --from developer`,
	}
	for _, command := range cases {
		if !isSamePaneDeveloperCommand(command) {
			t.Fatalf("expected same-pane developer command to be blocked: %s", command)
		}
	}
}

func TestDevRoleGuardPredicate_AllowsMasterSpawnAndReadOnlyDeveloperCommands(t *testing.T) {
	cases := []string{
		`export GG_ROLE=master; gg spawn worker --task TASK-123`,
		`GG_ROLE=developer gg task get TASK-123 --json`,
		`GG_ROLE=developer gg impact --compact internal/templates/dev-routing.md`,
		`gg tell master "TASK-123 ACK" --from developer --audience agents`,
	}
	for _, command := range cases {
		if isSamePaneDeveloperCommand(command) {
			t.Fatalf("expected command to be allowed: %s", command)
		}
	}
}

func TestParsePreToolUseBash_ReadsClaudeToolInputCommand(t *testing.T) {
	raw := []byte(`{"tool_name":"Bash","tool_input":{"command":"GG_ROLE=developer gg task start TASK-123"}}`)
	tool, command := parsePreToolUseBash(raw)
	if tool != "Bash" {
		t.Fatalf("tool=%q, want Bash", tool)
	}
	if command != "GG_ROLE=developer gg task start TASK-123" {
		t.Fatalf("command=%q", command)
	}
}
