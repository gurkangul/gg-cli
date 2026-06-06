package agenthooks

import (
	"regexp"
	"strings"
	"testing"
)

// BUG-083: the inbox hook's grep must match the actual inbox headers — including
// the compact 'inbox — N unread' form emitted under env.GG_AGENT — and must NOT
// match an empty inbox, so a turn with no unread mail injects nothing.
func TestInboxHookGrepMatchesRealHeaders(t *testing.T) {
	// The pattern embedded in claudeInboxCommand.
	re := regexp.MustCompile(`[1-9][0-9]* unread`)

	matchCases := []string{
		"inbox — 3 unread",    // compact renderer (writeInboxCompact)
		"INBOX (3 unread):",   // non-compact renderer (writeMessages)
		"inbox — 12 unread\n", // multi-digit
	}
	for _, c := range matchCases {
		if !re.MatchString(c) {
			t.Errorf("grep should match %q (BUG-083: hook never injects)", c)
		}
	}

	noMatchCases := []string{
		"inbox — 0 unread",
		"INBOX (0 unread):",
		"No unread messages.",
		"",
	}
	for _, c := range noMatchCases {
		if re.MatchString(c) {
			t.Errorf("grep should NOT match %q (would inject on empty inbox)", c)
		}
	}

	// The command must actually carry that pattern + v2 marker + conditional role.
	if !strings.Contains(claudeInboxCommand, `grep -qE '[1-9][0-9]* unread'`) {
		t.Error("claudeInboxCommand lost the corrected grep pattern")
	}
	if !strings.Contains(claudeInboxCommand, "gg-inbox-hook-v2") {
		t.Error("claudeInboxCommand missing v2 marker — stale installs won't be rewritten")
	}
	if !strings.Contains(claudeInboxCommand, `${GG_ROLE:+--role "$GG_ROLE"}`) {
		t.Error("claudeInboxCommand should only pass --role when GG_ROLE is set")
	}
	if strings.Contains(claudeInboxCommand, "--advance-cursor") {
		t.Error("preview hook must not advance the cursor")
	}
}
