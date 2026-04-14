package cmd

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/gurkangul/gg-cli/internal/store"
	"github.com/spf13/cobra"
)

var inboxCmd = &cobra.Command{
	Use:   "inbox",
	Short: "Read unread messages",
	Long: `Show unread messages in the agent inbox.

Examples:
  gg inbox                        # show all unread, mark as read
  gg inbox --peek                 # view without marking as read
  gg inbox --since 2h             # only messages from last 2 hours
  gg inbox --older-than 7d        # dismiss messages older than 7 days
  gg inbox --dismiss-all          # mark all unread as read, no output
  gg inbox --group-by sender      # group messages by sender role`,
	RunE: runInbox,
}

var (
	inboxRole       string
	inboxPeek       bool
	inboxDismissAll bool
	inboxSince      string
	inboxOlderThan  string
	inboxGroupBy    string
)

func init() {
	inboxCmd.Flags().StringVar(&inboxRole, "role", "", "filter by recipient role")
	inboxCmd.Flags().BoolVar(&inboxPeek, "peek", false, "view messages without marking as read")
	inboxCmd.Flags().BoolVar(&inboxDismissAll, "dismiss-all", false, "mark all unread messages as read without printing them")
	inboxCmd.Flags().StringVar(&inboxSince, "since", "", "only show messages newer than duration (e.g. 2h, 7d, 30m)")
	inboxCmd.Flags().StringVar(&inboxOlderThan, "older-than", "", "dismiss (mark read) messages older than duration without showing them")
	inboxCmd.Flags().StringVar(&inboxGroupBy, "group-by", "", "group output by field: sender")
	rootCmd.AddCommand(inboxCmd)
}

func runInbox(cmd *cobra.Command, args []string) error {
	d, err := loadDeps(false)
	if err != nil {
		return err
	}
	defer d.Close()

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	// --dismiss-all: mark everything as read and exit.
	if inboxDismissAll {
		n, err := d.store.DismissAll(ctx, inboxRole)
		if err != nil {
			return fmt.Errorf("dismiss all: %w", err)
		}
		if n == 0 {
			fmt.Println("No unread messages.")
		} else {
			fmt.Printf("✓ Dismissed %d message(s).\n", n)
		}
		return nil
	}

	messages, err := d.store.GetInbox(ctx, inboxRole)
	if err != nil {
		return fmt.Errorf("get inbox: %w", err)
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
			if markErr := d.store.MarkMessagesRead(ctx, oldIDs); markErr != nil {
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

	printMessages(messages, inboxGroupBy)

	if inboxPeek {
		return nil
	}
	ids := make([]string, len(messages))
	for i, m := range messages {
		ids[i] = m.ID
	}
	if err := d.store.MarkMessagesRead(ctx, ids); err != nil {
		return fmt.Errorf("mark read: %w", err)
	}
	return nil
}

func printMessages(messages []store.Message, groupBy string) {
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
			fmt.Printf("\nFrom: %s (%d)\n", role, len(msgs))
			fmt.Println(strings.Repeat("─", 40))
			for _, m := range msgs {
				printMessage(m)
			}
		}
		return
	}

	fmt.Printf("INBOX (%d unread):\n", len(messages))
	for _, m := range messages {
		printMessage(m)
	}
}

func printMessage(m store.Message) {
	fmt.Printf("  [%s → %s] %s\n", m.FromRole, m.ToRole, m.Content)
	if m.TaskID != "" {
		fmt.Printf("    Task: %s\n", m.TaskID)
	}
	if m.CreatedAt != "" {
		ts, err := time.Parse(time.RFC3339, m.CreatedAt)
		if err == nil {
			fmt.Printf("    %s\n", ts.Local().Format("2006-01-02 15:04"))
		}
	}
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
