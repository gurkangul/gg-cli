// Package cmd — tests for role-targeted imperative marker in printMessages.
package cmd

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/gurkangul/gg-cli/internal/store"
	"github.com/gurkangul/gg-cli/internal/templates"
)

// captureStdout redirects os.Stdout to a buffer for the duration of fn.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	old := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = old }()

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("close pipe: %v", err)
	}
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("copy: %v", err)
	}
	return buf.String()
}

func TestInbox_ActionRequired_ToRole(t *testing.T) {
	t.Setenv("GG_ROLE", "gsd")

	msgs := []store.Message{
		{FromRole: "claude-code", ToRole: "gsd", Content: "please review TASK-010"},
	}
	out := captureStdout(t, func() {
		printMessages(msgs, "")
	})

	if !strings.Contains(out, "[ACTION REQUIRED]") {
		t.Errorf("expected [ACTION REQUIRED] for to_role=gsd when GG_ROLE=gsd, got:\n%s", out)
	}
}

func TestInbox_ActionRequired_MentionInContent(t *testing.T) {
	t.Setenv("GG_ROLE", "gsd")

	msgs := []store.Message{
		{FromRole: "claude-code", ToRole: "all", Content: "@gsd please take TASK-010"},
	}
	out := captureStdout(t, func() {
		printMessages(msgs, "")
	})

	if !strings.Contains(out, "[ACTION REQUIRED]") {
		t.Errorf("expected [ACTION REQUIRED] for @gsd mention when GG_ROLE=gsd, got:\n%s", out)
	}
}

func TestInbox_InfoPrefix_OtherRole(t *testing.T) {
	t.Setenv("GG_ROLE", "gsd")

	msgs := []store.Message{
		{FromRole: "claude-code", ToRole: "all", Content: "TASK-010 done"},
	}
	out := captureStdout(t, func() {
		printMessages(msgs, "")
	})

	if !strings.Contains(out, "[INFO]") {
		t.Errorf("expected [INFO] prefix for non-targeted message when GG_ROLE=gsd, got:\n%s", out)
	}
	if strings.Contains(out, "[ACTION REQUIRED]") {
		t.Errorf("unexpected [ACTION REQUIRED] for non-targeted message, got:\n%s", out)
	}
}

func TestInbox_TwoSections_WhenBothExist(t *testing.T) {
	t.Setenv("GG_ROLE", "gsd")

	msgs := []store.Message{
		{FromRole: "claude-code", ToRole: "gsd", Content: "assignment"},
		{FromRole: "claude-code", ToRole: "all", Content: "broadcast"},
	}
	out := captureStdout(t, func() {
		printMessages(msgs, "")
	})

	if !strings.Contains(out, "ASSIGNMENTS REQUIRING ACTION") {
		t.Errorf("expected ASSIGNMENTS section header, got:\n%s", out)
	}
	if !strings.Contains(out, "INFO") {
		t.Errorf("expected INFO section header, got:\n%s", out)
	}
}

func TestInbox_FlatList_WhenGGRoleUnset(t *testing.T) {
	t.Setenv("GG_ROLE", "")

	msgs := []store.Message{
		{FromRole: "claude-code", ToRole: "gsd", Content: "assignment"},
		{FromRole: "claude-code", ToRole: "all", Content: "broadcast"},
	}
	out := captureStdout(t, func() {
		printMessages(msgs, "")
	})

	if strings.Contains(out, "[ACTION REQUIRED]") || strings.Contains(out, "[INFO]") {
		t.Errorf("GG_ROLE unset should produce flat list without prefixes, got:\n%s", out)
	}
	if !strings.Contains(out, fmt.Sprintf("INBOX (%d unread)", len(msgs))) {
		t.Errorf("expected flat INBOX header, got:\n%s", out)
	}
}

func TestInbox_ContractContainsInboxObeyRule(t *testing.T) {
	contract := templates.AgentContract
	checks := []struct {
		name string
		want string
	}{
		{"durable memory sync framing", "The mandatory rule is durable memory sync"},
		{"future-agent continuity", "anything future agents must know goes into gg"},
		{"handoff capture", "handoffs"},
		{"role-scoped inbox orientation", "gg inbox --role \"$GG_ROLE\" --peek"},
	}
	for _, check := range checks {
		if !strings.Contains(contract, check.want) {
			t.Errorf("agent-contract.md missing %s %q:\n%s", check.name, check.want, contract)
		}
	}
}

func TestValidateInboxCursorAdvanceRequiresRole(t *testing.T) {
	advance, err := validateInboxCursorAdvance("", false, true)
	if err == nil {
		t.Fatal("expected role-less --advance-cursor to be rejected")
	}
	if advance {
		t.Fatal("unsafe cursor advance should not be effective")
	}
	if !strings.Contains(err.Error(), "--role") {
		t.Fatalf("error should mention --role, got %q", err.Error())
	}

	advance, err = validateInboxCursorAdvance("", true, true)
	if err == nil {
		t.Fatal("expected role-less --peek --advance-cursor to be rejected before it can hide assignments")
	}
	if advance {
		t.Fatal("role-less peek+advance should not be effective")
	}
}

func TestValidateInboxCursorAdvancePeekDoesNotAdvance(t *testing.T) {
	advance, err := validateInboxCursorAdvance("reviewer", true, true)
	if err != nil {
		t.Fatalf("peek+advance should be treated as safe no-op, got %v", err)
	}
	if advance {
		t.Fatal("--peek must not advance the cursor")
	}
}

func TestValidateInboxCursorAdvanceAllowsRoleScopedAdvance(t *testing.T) {
	advance, err := validateInboxCursorAdvance("reviewer", false, true)
	if err != nil {
		t.Fatalf("role-scoped advance should be allowed, got %v", err)
	}
	if !advance {
		t.Fatal("role-scoped advance should be effective")
	}
}
