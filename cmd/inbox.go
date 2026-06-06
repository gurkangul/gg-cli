package cmd

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/identity"
	"github.com/gurkangul/gg-cli/internal/session"
	"github.com/gurkangul/gg-cli/internal/store"
	"github.com/spf13/cobra"
)

var inboxCmd = &cobra.Command{
	Use:   "inbox",
	Short: "Read unread messages",
	Long: `Show unread messages in the agent inbox.

By default, agent-to-agent broadcast messages (audience=agents) are hidden.
Use --include-agents to see them.

Examples:
  gg inbox --role reviewer --peek # safe role-scoped read without marking global read state
  gg inbox --include-agents       # include agent-to-agent status broadcasts
  gg inbox --since 2h             # only messages from last 2 hours
  gg inbox --role reviewer --since-cursor --advance-cursor
                                  # cursor read; requires --role and is incompatible with --peek
  gg inbox --older-than 7d        # dismiss messages older than 7 days
  gg inbox --dismiss-all          # mark all unread messages as read without printing them
  gg inbox --group-by sender      # group messages by sender role`,
	RunE: runInbox,
}

var (
	inboxRole          string
	inboxPeek          bool
	inboxDismissAll    bool
	inboxSince         string
	inboxOlderThan     string
	inboxGroupBy       string
	inboxIncludeAgents bool
	inboxSinceCursor   bool
	inboxAdvanceCursor bool
	inboxCompact       bool
)

func init() {
	inboxCmd.Flags().StringVar(&inboxRole, "role", "", "filter by recipient role")
	inboxCmd.Flags().BoolVar(&inboxPeek, "peek", false, "view messages without marking as read")
	inboxCmd.Flags().BoolVar(&inboxDismissAll, "dismiss-all", false, "mark all unread messages as read without printing them")
	inboxCmd.Flags().StringVar(&inboxSince, "since", "", "only show messages newer than duration (e.g. 2h, 7d, 30m)")
	inboxCmd.Flags().StringVar(&inboxOlderThan, "older-than", "", "dismiss (mark read) messages older than duration without showing them")
	inboxCmd.Flags().StringVar(&inboxGroupBy, "group-by", "", "group output by field: sender")
	inboxCmd.Flags().BoolVar(&inboxIncludeAgents, "include-agents", false, "show agent-to-agent broadcasts (hidden by default)")
	inboxCmd.Flags().BoolVar(&inboxSinceCursor, "since-cursor", false, "only show messages newer than the stored per-agent cursor")
	inboxCmd.Flags().BoolVar(&inboxAdvanceCursor, "advance-cursor", false, "after render, advance cursor; requires --role and is ignored with --peek")
	inboxCmd.Flags().BoolVar(&inboxCompact, "compact", false, "one line per message — drops timestamp precision and action-required split to preserve agent context window")
	rootCmd.AddCommand(inboxCmd)
}

// resolveInboxAgent returns the agent identity for cursor + per-recipient
// read-state operations. Returns empty string when identity cannot be
// determined — callers treat empty as "skip cursor logic silently"
// (backwards-compatible). BUG-084: resolves a per-session id under Claude Code
// so two tabs do not share one inbox read-state (ties to BUG-082).
func resolveInboxAgent() string {
	return strings.TrimSpace(identity.Agent())
}

func runInbox(cmd *cobra.Command, args []string) error {
	advanceCursor, cursorErr := validateInboxCursorAdvance(inboxRole, inboxPeek, inboxAdvanceCursor)
	if cursorErr != nil {
		return cursorErr
	}

	d, err := loadDeps(false)
	if err != nil {
		return err
	}
	defer d.Close()

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	reader := resolveInboxAgent()

	// --dismiss-all: mark everything as read and exit.
	if inboxDismissAll {
		n, err := d.store.DismissAll(ctx, inboxRole, reader)
		if err != nil {
			return fmt.Errorf("dismiss all: %w", err)
		}
		if n == 0 {
			fmt.Println("No unread messages.")
		} else {
			fmt.Printf("✓ Dismissed %d message(s).\n", n)
			if advanceCursor {
				advanceCursorToNow(d)
			}
		}
		return nil
	}

	messages, err := d.store.GetInbox(ctx, inboxRole, !inboxIncludeAgents, reader)
	if err != nil {
		return fmt.Errorf("get inbox: %w", err)
	}

	// --since-cursor: filter to messages newer than the stored per-agent cursor.
	// Silently skipped when GG_AGENT is unset or runtimeDir is unavailable.
	if inboxSinceCursor {
		agent := resolveInboxAgent()
		if agent != "" {
			if cfg, cfgErr := config.Load(); cfgErr == nil {
				if runtimeDir, rtErr := cfg.RuntimeDir(); rtErr == nil {
					cursor, ok, rdErr := session.Read(runtimeDir, agent)
					if rdErr == nil && ok {
						filtered := messages[:0]
						for _, m := range messages {
							ts, parseErr := time.Parse(time.RFC3339, m.CreatedAt)
							if parseErr != nil || ts.After(cursor) {
								filtered = append(filtered, m)
							}
						}
						messages = filtered
					}
				}
			}
		}
	}

	// Parse --older-than: dismiss matching messages silently.
	if inboxOlderThan != "" {
		cutoff, err := parseDuration(inboxOlderThan)
		if err != nil {
			return fmt.Errorf("--older-than: %w", err)
		}
		threshold := time.Now().UTC().Add(-cutoff)
		var oldIDs []string
		remaining := messages[:0]
		for _, m := range messages {
			ts, parseErr := time.Parse(time.RFC3339, m.CreatedAt)
			if parseErr == nil && ts.Before(threshold) {
				oldIDs = append(oldIDs, m.ID)
			} else {
				remaining = append(remaining, m)
			}
		}
		if len(oldIDs) > 0 {
			if markErr := d.store.MarkMessagesRead(ctx, oldIDs, reader); markErr != nil {
				return fmt.Errorf("dismiss old: %w", markErr)
			}
			fmt.Printf("✓ Dismissed %d message(s) older than %s.\n", len(oldIDs), inboxOlderThan)
		}
		messages = remaining
	}

	// Parse --since: filter to messages within duration.
	if inboxSince != "" {
		cutoff, err := parseDuration(inboxSince)
		if err != nil {
			return fmt.Errorf("--since: %w", err)
		}
		threshold := time.Now().UTC().Add(-cutoff)
		filtered := messages[:0]
		for _, m := range messages {
			ts, parseErr := time.Parse(time.RFC3339, m.CreatedAt)
			if parseErr != nil || !ts.Before(threshold) {
				filtered = append(filtered, m)
			}
		}
		messages = filtered
	}

	if len(messages) == 0 {
		fmt.Println("No unread messages.")
		return nil
	}

	// Sort newest first.
	sort.Slice(messages, func(i, j int) bool {
		return messages[i].CreatedAt > messages[j].CreatedAt
	})

	if isCompactActive(cmd) {
		emitCompact(cmd, "inbox",
			func(w io.Writer) {
				myRole := strings.TrimSpace(os.Getenv("GG_ROLE"))
				writeMessages(w, messages, inboxGroupBy, myRole)
			},
			func(w io.Writer) { writeInboxCompact(w, messages) },
			compactRendererV_inbox,
		)
	} else {
		printMessages(messages, inboxGroupBy)
	}

	// Advance cursor to the newest message shown (or now if no messages).
	if advanceCursor {
		advanceCursorToMax(d, messages)
	}

	if inboxPeek {
		return nil
	}
	ids := make([]string, len(messages))
	for i, m := range messages {
		ids[i] = m.ID
	}
	if err := d.store.MarkMessagesRead(ctx, ids, reader); err != nil {
		return fmt.Errorf("mark read: %w", err)
	}
	return nil
}

func validateInboxCursorAdvance(role string, peek, advance bool) (bool, error) {
	if !advance {
		return false, nil
	}
	if strings.TrimSpace(role) == "" {
		return false, fmt.Errorf("--advance-cursor requires --role <role>; role-less cursor advance can hide role-targeted assignments")
	}
	if peek {
		return false, nil
	}
	return true, nil
}

// advanceCursorToMax writes the cursor to max(CreatedAt) of shown messages,
// or now() when the message list is empty. Silently no-ops on any error or
// when GG_AGENT / runtimeDir are unavailable.
func advanceCursorToMax(_ *deps, messages []store.Message) {
	agent := resolveInboxAgent()
	if agent == "" {
		return
	}
	cfg, err := config.Load()
	if err != nil {
		return
	}
	runtimeDir, err := cfg.RuntimeDir()
	if err != nil {
		return
	}
	ts := time.Now().UTC()
	for _, m := range messages {
		if t, parseErr := time.Parse(time.RFC3339, m.CreatedAt); parseErr == nil && t.After(ts) {
			ts = t
		}
	}
	_ = session.Write(runtimeDir, agent, ts)
}

// advanceCursorToNow writes cursor = now(). Used after dismiss-all.
func advanceCursorToNow(_ *deps) {
	agent := resolveInboxAgent()
	if agent == "" {
		return
	}
	cfg, err := config.Load()
	if err != nil {
		return
	}
	runtimeDir, err := cfg.RuntimeDir()
	if err != nil {
		return
	}
	_ = session.Write(runtimeDir, agent, time.Now().UTC())
}

func printMessages(messages []store.Message, groupBy string) {
	writeMessages(os.Stdout, messages, groupBy, strings.TrimSpace(os.Getenv("GG_ROLE")))
}

// writeMessages renders the default (rich) inbox view to w. Split from
// printMessages so the compact-baseline measurement can capture the exact
// bytes the default view would have printed without a stdout redirect.
func writeMessages(w io.Writer, messages []store.Message, groupBy, myRole string) {
	if groupBy == "sender" {
		byRole := make(map[string][]store.Message)
		var order []string
		seen := make(map[string]bool)
		for _, m := range messages {
			if !seen[m.FromRole] {
				order = append(order, m.FromRole)
				seen[m.FromRole] = true
			}
			byRole[m.FromRole] = append(byRole[m.FromRole], m)
		}
		for _, role := range order {
			msgs := byRole[role]
			fmt.Fprintf(w, "\nFrom: %s (%d)\n", role, len(msgs))
			fmt.Fprintln(w, strings.Repeat("─", 40))
			for _, m := range msgs {
				writeMessage(w, m, myRole)
			}
		}
		return
	}

	// When GG_ROLE is set, split messages into action-required vs info.
	if myRole != "" {
		var actions, infos []store.Message
		for _, m := range messages {
			if isActionRequired(m, myRole) {
				actions = append(actions, m)
			} else {
				infos = append(infos, m)
			}
		}
		if len(actions) > 0 && len(infos) > 0 {
			fmt.Fprintf(w, "ASSIGNMENTS REQUIRING ACTION (%d):\n", len(actions))
			for _, m := range actions {
				writeMessage(w, m, myRole)
			}
			fmt.Fprintf(w, "\nINFO (%d):\n", len(infos))
			for _, m := range infos {
				writeMessage(w, m, myRole)
			}
			return
		}
	}

	fmt.Fprintf(w, "INBOX (%d unread):\n", len(messages))
	for _, m := range messages {
		writeMessage(w, m, myRole)
	}
}

// writeInboxCompact emits one line per message — drops the action-required
// split, the sender grouping, and the rich multi-line per-message format.
func writeInboxCompact(w io.Writer, messages []store.Message) {
	fmt.Fprintf(w, "inbox — %d unread\n", len(messages))
	for _, m := range messages {
		fmt.Fprintln(w, compactMessageLine(m))
	}
}

// isActionRequired returns true when a message is directly targeting myRole
// either via to_role or an @role mention in content.
func isActionRequired(m store.Message, myRole string) bool {
	if myRole == "" {
		return false
	}
	if strings.EqualFold(m.ToRole, myRole) {
		return true
	}
	lower := strings.ToLower(m.Content)
	return strings.Contains(lower, "@"+strings.ToLower(myRole))
}

func printMessage(m store.Message, myRole string) {
	writeMessage(os.Stdout, m, myRole)
}

func writeMessage(w io.Writer, m store.Message, myRole string) {
	prefix := ""
	if myRole != "" {
		if isActionRequired(m, myRole) {
			prefix = "[ACTION REQUIRED] "
		} else {
			prefix = "[INFO] "
		}
	}
	fmt.Fprintf(w, "  %s[%s → %s] %s\n", prefix, m.FromRole, m.ToRole, highlightMentions(m.Content))
	if m.TaskID != "" {
		fmt.Fprintf(w, "    Task: %s\n", m.TaskID)
	}
	if m.CreatedAt != "" {
		ts, err := time.Parse(time.RFC3339, m.CreatedAt)
		if err == nil {
			fmt.Fprintf(w, "    %s\n", ts.Local().Format("2006-01-02 15:04"))
		}
	}
}

// highlightMentions wraps @role tokens with angle brackets for visual emphasis.
func highlightMentions(content string) string {
	return mentionRe.ReplaceAllString(content, "<@${1}>")
}

// parseDuration parses durations like "2h", "7d", "30m".
// Extends Go's time.ParseDuration with day support ("d" suffix).
func parseDuration(s string) (time.Duration, error) {
	if strings.HasSuffix(s, "d") {
		days := strings.TrimSuffix(s, "d")
		var n int
		if _, err := fmt.Sscanf(days, "%d", &n); err != nil {
			return 0, fmt.Errorf("invalid day value %q", s)
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	return time.ParseDuration(s)
}
