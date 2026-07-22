package enforcement

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/gurkangul/gg-cli/internal/store"
)

// defaultInboxGateWindow bounds how far back the gate blocks on role-targeted
// handoffs. BUG-103: once identity.Agent() resolves per-tab (a fresh session id
// with an empty read_by), an unbounded gate would re-block every accumulated
// handoff for every new tab — BUG-102 reached from the other side. The window
// makes the fresh-tab candidate set O(recent activity), not O(history), by a
// wall-clock threshold independent of identity/cursor/read_by. 14d is generous
// enough that a weekend or sprint gap never drops a live handoff by age.
const defaultInboxGateWindow = 14 * 24 * time.Hour

// inboxGateWindow returns the recency window and whether it is enabled.
// Unset/empty => 14d (ON). "0"/"off" => disabled (legacy unbounded: block
// role-targeted handoffs of any age — the max-safety escape hatch). Any parseable
// duration overrides. A parse failure falls back to the default (bounded), never
// to unbounded, so a typo cannot silently resurrect the whole backlog.
func inboxGateWindow(getenv func(string) string) (time.Duration, bool) {
	raw := strings.TrimSpace(getenv("GG_INBOX_GATE_WINDOW"))
	switch strings.ToLower(raw) {
	case "":
		return defaultInboxGateWindow, true
	case "0", "off":
		return 0, false
	}
	d, err := parseDayDuration(raw)
	if err != nil || d <= 0 {
		return defaultInboxGateWindow, true
	}
	return d, true
}

// parseDayDuration extends time.ParseDuration with a "d" (day) suffix (e.g. "7d").
// Duplicated locally because cmd.parseDuration is package cmd and internal/
// enforcement must not import cmd.
func parseDayDuration(s string) (time.Duration, error) {
	if strings.HasSuffix(s, "d") {
		var n int
		if _, err := fmt.Sscanf(strings.TrimSuffix(s, "d"), "%d", &n); err != nil {
			return 0, fmt.Errorf("invalid day value %q", s)
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	return time.ParseDuration(s)
}

// inboxChecker is a narrow interface satisfied by *store.Client. Injected in
// tests to avoid real Qdrant calls.
type inboxChecker interface {
	GetInbox(ctx context.Context, role string, humanOnly bool, reader string) ([]store.Message, error)
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
	// Reader is the per-recipient read-state key the gate filtered by. Empty
	// means an unidentified caller, which falls back to legacy global-read
	// semantics — the same state an anonymous dismiss writes.
	Reader string
}

// CheckInboxGate evaluates whether role-targeted handoffs need attention before
// a state-changing command writes new shared memory. It returns a result
// indicating whether the caller should be blocked until the durable handoff or
// evidence context is read, replied to, or consciously bypassed.
//
// reader is the per-recipient read-state key — the SAME identity gg inbox writes
// into a message's read_by set (identity.Agent(), not the role string). Passing
// it makes the gate query byte-identical to the user-facing inbox, so an agent
// that has actually read its mail clears the gate (BUG-102). reader="" degrades
// to legacy global-read semantics, which is exactly what an anonymous dismiss
// also writes, so gate and remedy still agree. GetInbox performs no writes, so
// supplying a reader is a pure filter and never consumes another agent's message.
//
// When GG_ALLOW_INBOX_SKIP is set the gate is always bypassed (result.Bypassed=true).
// When role is empty or enforcement is disabled the gate is skipped (result.Blocked=false).
func CheckInboxGate(ctx context.Context, client inboxChecker, role, reader string) (InboxGateResult, error) {
	if !Enabled() || role == "" {
		return InboxGateResult{}, nil
	}

	// Bypass path.
	if reason := strings.TrimSpace(os.Getenv("GG_ALLOW_INBOX_SKIP")); reason != "" {
		return InboxGateResult{Bypassed: true, BypassReason: reason}, nil
	}

	msgs, err := client.GetInbox(ctx, role, false, reader)
	if err != nil {
		// Store unreachable — fail open so a down Qdrant doesn't block work.
		return InboxGateResult{}, nil
	}

	// BUG-103: bound the gate to a recency window so a fresh per-tab identity
	// leaves it satisfiable (see defaultInboxGateWindow). Computed once.
	window, windowed := inboxGateWindow(os.Getenv)
	var threshold time.Time
	if windowed {
		threshold = time.Now().Add(-window)
	}

	// Filter to only role-targeted messages (to_role == role or @role mention).
	var targeted []store.Message
	lowerRole := strings.ToLower(role)
	for _, m := range msgs {
		// Recency window first: an old handoff is skipped before the assignment-
		// resolved GetTask round-trip. Keep-on-parse-error mirrors cmd/inbox.go's
		// fail-open convention (and preserves tests whose messages have no
		// CreatedAt) — an unparseable timestamp never silently drops a message.
		if windowed {
			if ts, perr := time.Parse(time.RFC3339, m.CreatedAt); perr == nil && ts.Before(threshold) {
				continue
			}
		}
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
		return InboxGateResult{Reader: reader}, nil
	}
	return InboxGateResult{Blocked: true, Count: len(targeted), Messages: targeted, Reader: reader}, nil
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
	who := result.Reader
	if who == "" {
		who = "this (unidentified) process"
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "MISSING DURABLE HANDOFF CONTEXT: %d message(s) unread by %s for role %s\n", result.Count, who, role)
	for _, m := range result.Messages {
		preview := m.Content
		if len(preview) > 80 {
			preview = preview[:80] + "…"
		}
		fmt.Fprintf(&sb, "  [%s → %s] %s\n", m.FromRole, m.ToRole, preview)
	}
	// The remedy names a read that actually clears the gate: gg inbox records
	// the read in this reader's own read_by set (BUG-082), the same key the gate
	// now filters by (BUG-102). --include-agents is REQUIRED: the gate scans all
	// audiences (GetInbox humanOnly=false), so an agent-to-agent handoff
	// (gg tell <role> --audience agents) blocks here but is hidden from the
	// default human-facing `gg inbox`; without --include-agents the advertised
	// remedy would print "No unread messages." and never clear the block.
	fmt.Fprintf(&sb, "Run: gg inbox --role %s --include-agents   (marks these read for %s only, which clears this gate)\n", role, who)
	sb.WriteString("Read or respond so future agents can see the blocker, decision, review request, or evidence path. If you already handled it elsewhere, set GG_ALLOW_INBOX_SKIP=<reason> to record an audited bypass.")
	return sb.String()
}
