package enforcement

import (
	"context"
	"fmt"
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

func (f *fakeInboxChecker) GetInbox(_ context.Context, role string, _ bool) ([]store.Message, error) {
	if f.err != nil {
		return nil, f.err
	}
	if role == "" {
		return f.messages, nil
	}
	var out []store.Message
	for _, m := range f.messages {
		if m.ToRole == role || m.ToRole == "all" {
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
	result, err := CheckInboxGate(context.Background(), client, "")
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
	result, err := CheckInboxGate(context.Background(), client, "gsd")
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
	client := &fakeInboxChecker{messages: []store.Message{
		{FromRole: "claude-code", ToRole: "gsd", Content: "please take TASK-010"},
	}}
	result, err := CheckInboxGate(context.Background(), client, "gsd")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Blocked {
		t.Errorf("message to_role=gsd should block gsd, got Blocked=false")
	}
	if result.Count != 1 {
		t.Errorf("Count want 1, got %d", result.Count)
	}
}

func TestCheckInboxGate_DoneTaskAssignment_NotBlocked(t *testing.T) {
	client := &fakeInboxChecker{
		messages: []store.Message{{FromRole: "implementer", ToRole: "reviewer", Content: "TASK-010 ready", TaskID: "TASK-010"}},
		tasks:    map[string]*store.Task{"TASK-010": {ID: "TASK-010", Status: "done"}},
	}
	result, err := CheckInboxGate(context.Background(), client, "reviewer")
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
	result, err := CheckInboxGate(context.Background(), client, "reviewer")
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
	result, err := CheckInboxGate(context.Background(), client, "reviewer")
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
	result, err := CheckInboxGate(context.Background(), client, "reviewer")
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
	result, err := CheckInboxGate(context.Background(), client, "implementer")
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
	result, err := CheckInboxGate(context.Background(), client, "gsd")
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
	result, err := CheckInboxGate(context.Background(), client, "gsd")
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
	result, err := CheckInboxGate(context.Background(), client, "gsd")
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
	result, err := CheckInboxGate(context.Background(), client, "gsd")
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
		Messages: []store.Message{
			{FromRole: "implementer", ToRole: "reviewer", Content: "TASK-123 ready. Evidence: commands=go test ./...; gaps=none"},
		},
	})
	for _, want := range []string{"MISSING DURABLE HANDOFF CONTEXT", "future agents", "evidence path", "GG_ALLOW_INBOX_SKIP"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("formatted block message missing %q:\n%s", want, msg)
		}
	}
}

var errStoreDown = fmt.Errorf("store unreachable")
