package enforcement

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/gurkangul/gg-cli/internal/store"
)

// inboxChecker is a narrow interface satisfied by *store.Client. Injected in
// tests to avoid real Qdrant calls.
type inboxChecker interface {
	GetInbox(ctx context.Context, role string, humanOnly bool) ([]store.Message, error)
	GetTask(ctx context.Context, taskID string) (*store.Task, error)
}

// InboxGateResult is the outcome of a single inbox-gate evaluation.
type InboxGateResult struct {
	// Blocked is true when the calling agent has unread role-targeted assignments.
	Blocked bool
	// Count is the number of unread role-targeted messages found.
	Count int
	// Messages contains the blocking messages (empty when Blocked=false).
	Messages []store.Message
	// Bypassed is true when GG_ALLOW_INBOX_SKIP was set and the gate was skipped.
	Bypassed bool
	// BypassReason is the value of GG_ALLOW_INBOX_SKIP (empty when not bypassed).
	BypassReason string
}

// CheckInboxGate evaluates the inbox obey rule for the given role.
// It returns a result indicating whether the caller should be blocked.
// When GG_ALLOW_INBOX_SKIP is set the gate is always bypassed (result.Bypassed=true).
// When role is empty or enforcement is disabled the gate is skipped (result.Blocked=false).
func CheckInboxGate(ctx context.Context, client inboxChecker, role string) (InboxGateResult, error) {
	if !Enabled() || role == "" {
		return InboxGateResult{}, nil
	}

	// Bypass path.
	if reason := strings.TrimSpace(os.Getenv("GG_ALLOW_INBOX_SKIP")); reason != "" {
		return InboxGateResult{Bypassed: true, BypassReason: reason}, nil
	}

	msgs, err := client.GetInbox(ctx, role, false)
	if err != nil {
		// Store unreachable — fail open so a down Qdrant doesn't block work.
		return InboxGateResult{}, nil
	}

	// Filter to only role-targeted messages (to_role == role or @role mention).
	var targeted []store.Message
	lowerRole := strings.ToLower(role)
	for _, m := range msgs {
		if assignmentResolved(ctx, client, m) {
			continue
		}
		if strings.EqualFold(m.ToRole, role) {
			targeted = append(targeted, m)
			continue
		}
		if strings.Contains(strings.ToLower(m.Content), "@"+lowerRole) {
			targeted = append(targeted, m)
		}
	}

	if len(targeted) == 0 {
		return InboxGateResult{}, nil
	}
	return InboxGateResult{Blocked: true, Count: len(targeted), Messages: targeted}, nil
}

func assignmentResolved(ctx context.Context, client inboxChecker, m store.Message) bool {
	if strings.TrimSpace(m.TaskID) == "" {
		return false
	}
	t, err := client.GetTask(ctx, m.TaskID)
	if err != nil {
		// Cancelled tasks are removed from the live projection; treat a missing
		// task-linked assignment as resolved instead of permanently blocking new work.
		return strings.Contains(strings.ToLower(err.Error()), "not found")
	}
	switch strings.ToLower(strings.TrimSpace(t.Status)) {
	case "done", "cancelled":
		return true
	}
	return strings.EqualFold(strings.TrimSpace(t.ReviewStatus), "rejected")
}

// FormatBlockMessage formats the inbox-gate block error message shown to the agent.
func FormatBlockMessage(role string, result InboxGateResult) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "ACTION REQUIRED: %d unread assignment(s) for role %s\n", result.Count, role)
	for _, m := range result.Messages {
		preview := m.Content
		if len(preview) > 80 {
			preview = preview[:80] + "…"
		}
		fmt.Fprintf(&sb, "  [%s → %s] %s\n", m.FromRole, m.ToRole, preview)
	}
	sb.WriteString("Handle these assignments first, or set GG_ALLOW_INBOX_SKIP=<reason> to bypass.")
	return sb.String()
}
