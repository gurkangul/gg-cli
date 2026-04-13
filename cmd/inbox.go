package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

var inboxCmd = &cobra.Command{
	Use:   "inbox",
	Short: "Read unread messages",
	RunE:  runInbox,
}

var inboxRole string

func init() {
	inboxCmd.Flags().StringVar(&inboxRole, "role", "", "filter by recipient role")
	rootCmd.AddCommand(inboxCmd)
}

func runInbox(cmd *cobra.Command, args []string) error {
	client, err := newStoreClient()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx := context.Background()
	messages, err := client.GetInbox(ctx, inboxRole)
	if err != nil {
		return fmt.Errorf("get inbox: %w", err)
	}

	if len(messages) == 0 {
		fmt.Println("No unread messages.")
		return nil
	}

	fmt.Printf("INBOX (%d unread):\n", len(messages))
	var ids []string
	for _, m := range messages {
		fmt.Printf("  [%s → %s] %s\n", m.FromRole, m.ToRole, m.Content)
		if m.TaskID != "" {
			fmt.Printf("    Task: %s\n", m.TaskID)
		}
		ids = append(ids, m.ID)
	}

	// Mark all displayed messages as read
	if err := client.MarkMessagesRead(ctx, ids); err != nil {
		return fmt.Errorf("mark read: %w", err)
	}

	return nil
}
