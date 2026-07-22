package enforcement

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/gurkangul/gg-cli/internal/store"
)

// fakeInboxChecker is a test double for the inboxChecker interface.
type fakeInboxChecker struct {
	messages []store.Message
	tasks    map[string]*store.Task
	err      error
}

// GetInbox mirrors store.Client.GetInbox's read-model: a message is excluded
// when its legacy global Read flag is set OR (for an identified reader) that
// reader appears in its per-recipient ReadBy set. BUG-102: the earlier fake
// discarded the reader argument entirely, so no test in this package could
// detect the gate ignoring read_by — the exact defect BUG-102 was.
func (f *fakeInboxChecker) GetInbox(_ context.Context, role string, _ bool, reader string) ([]store.Message, error) {
	if f.err != nil {
		return nil, f.err
	}
	var out []store.Message
	for _, m := range f.messages {
		if m.Read {
			continue
		}
		if reader != "" && slices.Contains(m.ReadBy, reader) {
			continue
		}
		if role == "" || m.ToRole == role || m.ToRole == "all" {
			out = append(out, m)
		}
	}
	return out, nil
}

func (f *fakeInboxChecker) GetTask(_ context.Context, taskID string) (*store.Task, error) {
	if f.tasks != nil {
		if t := f.tasks[taskID]; t != nil {
			return t, nil
		}
	}
	return nil, fmt.Errorf("task %s not found", taskID)
}

func TestCheckInboxGate_EmptyRole_Skip(t *testing.T) {
	client := &fakeInboxChecker{messages: []store.Message{
		{FromRole: "claude-code", ToRole: "gsd", Content: "do this"},
	}}
	result, err := CheckInboxGate(context.Background(), client, "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Blocked {
		t.Errorf("empty role should skip gate, got Blocked=true")
	}
}

func TestCheckInboxGate_NoTargetedMessages_NotBlocked(t *testing.T) {
	t.Setenv("GG_ROLE", "")
	client := &fakeInboxChecker{messages: []store.Message{
		{FromRole: "claude-code", ToRole: "all", Content: "broadcast"},
	}}
	result, err := CheckInboxGate(context.Background(), client, "gsd", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// "all" messages filtered by GetInbox when role="gsd" are returned only
	// when ToRole matches. fakeInboxChecker returns ToRole=="gsd" or "all" —
	// the test message has ToRole=="all". The gate checks to_role==role (exact)
	// or @role mention. "all" != "gsd" and no @gsd in content → not targeted.
	if result.Blocked {
		t.Errorf("non-targeted 'all' message should not block, got Blocked=true")
	}
}

func TestCheckInboxGate_ToRoleMatch_Blocked(t *testing.T) {
	// BUG-102: a role-targeted handoff blocks the recipient, but the gate now
	// filters by the SAME per-recipient read_by key gg inbox writes. So the same
	// fixture proves both the block AND that an agent who has read it (recorded
	// in read_by) is cleared, while a different agent who has not is still
	// blocked. Amended in place rather than adding a new test (no-unit-tests
	// policy: existing expectations may be reshaped when behavior changes; the
	// end-to-end proof lives in testdata/regression/bug-102.sh).
	msg := store.Message{FromRole: "claude-code", ToRole: "gsd", Content: "please take TASK-010", ReadBy: []string{"agent-a"}}
	client := &fakeInboxChecker{messages: []store.Message{msg}}

	// agent-b has not read it → still blocked, and the reader is surfaced.
	result, err := CheckInboxGate(context.Background(), client, "gsd", "agent-b")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Blocked {
		t.Errorf("message to_role=gsd unread by agent-b should block, got Blocked=false")
	}
	if result.Count != 1 {
		t.Errorf("Count want 1, got %d", result.Count)
	}
	if result.Reader != "agent-b" {
		t.Errorf("Reader want agent-b, got %q", result.Reader)
	}

	// agent-a already read it (read_by) → gate clears. Pre-fix this stayed
	// blocked because the gate ignored read_by (reader was hardcoded "").
	cleared, err := CheckInboxGate(context.Background(), client, "gsd", "agent-a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cleared.Blocked {
		t.Errorf("agent-a already read the handoff → gate must clear, got Blocked=true")
	}
}

func TestCheckInboxGate_DoneTaskAssignment_NotBlocked(t *testing.T) {
	client := &fakeInboxChecker{
		messages: []store.Message{{FromRole: "implementer", ToRole: "reviewer", Content: "TASK-010 ready", TaskID: "TASK-010"}},
		tasks:    map[string]*store.Task{"TASK-010": {ID: "TASK-010", Status: "done"}},
	}
	result, err := CheckInboxGate(context.Background(), client, "reviewer", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Blocked {
		t.Fatalf("done task assignment should not block, got %+v", result)
	}
}

func TestCheckInboxGate_CancelledTaskAssignment_NotBlocked(t *testing.T) {
	client := &fakeInboxChecker{
		messages: []store.Message{{FromRole: "implementer", ToRole: "reviewer", Content: "TASK-013 ready", TaskID: "TASK-013"}},
		tasks:    map[string]*store.Task{"TASK-013": {ID: "TASK-013", Status: "cancelled"}},
	}
	result, err := CheckInboxGate(context.Background(), client, "reviewer", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Blocked {
		t.Fatalf("cancelled task assignment should not block, got %+v", result)
	}
}

func TestCheckInboxGate_MissingTaskAssignment_NotBlocked(t *testing.T) {
	client := &fakeInboxChecker{
		messages: []store.Message{{FromRole: "implementer", ToRole: "reviewer", Content: "TASK-014 ready", TaskID: "TASK-014"}},
		tasks:    map[string]*store.Task{},
	}
	result, err := CheckInboxGate(context.Background(), client, "reviewer", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Blocked {
		t.Fatalf("missing task assignment should not block, got %+v", result)
	}
}

func TestCheckInboxGate_RejectedReviewAssignment_NotBlocked(t *testing.T) {
	client := &fakeInboxChecker{
		messages: []store.Message{{FromRole: "implementer", ToRole: "reviewer", Content: "TASK-011 ready", TaskID: "TASK-011"}},
		tasks:    map[string]*store.Task{"TASK-011": {ID: "TASK-011", Status: "ready_for_live", ReviewStatus: "rejected"}},
	}
	result, err := CheckInboxGate(context.Background(), client, "reviewer", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Blocked {
		t.Fatalf("rejected review assignment should not block, got %+v", result)
	}
}

func TestCheckInboxGate_ActiveTaskAssignment_StillBlocks(t *testing.T) {
	client := &fakeInboxChecker{
		messages: []store.Message{{FromRole: "planner", ToRole: "implementer", Content: "TASK-012 start", TaskID: "TASK-012"}},
		tasks:    map[string]*store.Task{"TASK-012": {ID: "TASK-012", Status: "in_progress"}},
	}
	result, err := CheckInboxGate(context.Background(), client, "implementer", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Blocked {
		t.Fatal("active task assignment should still block")
	}
}

func TestCheckInboxGate_MentionInContent_Blocked(t *testing.T) {
	client := &fakeInboxChecker{messages: []store.Message{
		{FromRole: "claude-code", ToRole: "all", Content: "@gsd please review PR-42"},
	}}
	result, err := CheckInboxGate(context.Background(), client, "gsd", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Blocked {
		t.Errorf("@gsd mention should block gsd, got Blocked=false")
	}
}

func TestCheckInboxGate_Bypass_NotBlocked(t *testing.T) {
	t.Setenv("GG_ALLOW_INBOX_SKIP", "bootstrap-session")
	client := &fakeInboxChecker{messages: []store.Message{
		{FromRole: "claude-code", ToRole: "gsd", Content: "action required"},
	}}
	result, err := CheckInboxGate(context.Background(), client, "gsd", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Blocked {
		t.Errorf("GG_ALLOW_INBOX_SKIP set should bypass gate, got Blocked=true")
	}
	if !result.Bypassed {
		t.Errorf("expected Bypassed=true")
	}
	if result.BypassReason != "bootstrap-session" {
		t.Errorf("BypassReason=%q, want bootstrap-session", result.BypassReason)
	}
}

func TestCheckInboxGate_StoreError_FailOpen(t *testing.T) {
	client := &fakeInboxChecker{err: errStoreDown}
	result, err := CheckInboxGate(context.Background(), client, "gsd", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Blocked {
		t.Errorf("store error should fail open (not block), got Blocked=true")
	}
}

func TestCheckInboxGate_EnforcementDisabled_Skip(t *testing.T) {
	t.Setenv(EnvVar, "off")
	client := &fakeInboxChecker{messages: []store.Message{
		{FromRole: "a", ToRole: "gsd", Content: "action"},
	}}
	result, err := CheckInboxGate(context.Background(), client, "gsd", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Blocked {
		t.Errorf("enforcement disabled should skip gate, got Blocked=true")
	}
}

func TestFormatBlockMessage_ExplainsDurableHandoffContext(t *testing.T) {
	msg := FormatBlockMessage("reviewer", InboxGateResult{
		Blocked: true,
		Count:   1,
		Reader:  "codex-1",
		Messages: []store.Message{
			{FromRole: "implementer", ToRole: "reviewer", Content: "TASK-123 ready. Evidence: commands=go test ./...; gaps=none"},
		},
	})
	// BUG-102: the advertised remedy must be one that actually clears the gate.
	// The gate scans all audiences (humanOnly=false), so the remedy must carry
	// --include-agents or it cannot clear an agents-audience blocker; and it must
	// name the reader whose read_by the read is recorded against.
	for _, want := range []string{"MISSING DURABLE HANDOFF CONTEXT", "future agents", "evidence path", "GG_ALLOW_INBOX_SKIP", "--include-agents", "codex-1"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("formatted block message missing %q:\n%s", want, msg)
		}
	}
}

var errStoreDown = fmt.Errorf("store unreachable")
