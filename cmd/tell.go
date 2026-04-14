package cmd

import (
	"fmt"
	"strings"

	"github.com/gurkangul/gg/internal/store"
	"github.com/spf13/cobra"
)

var tellCmd = &cobra.Command{
	Use:   `tell "role" "message"`,
	Short: "Send a message to another agent role",
	Args:  cobra.ExactArgs(2),
	RunE:  runTell,
}

var (
	tellFrom string
	tellTask string
)

func init() {
	tellCmd.Flags().StringVar(&tellFrom, "from", "user", "sender role")
	tellCmd.Flags().StringVar(&tellTask, "task", "", "related task ID")
	rootCmd.AddCommand(tellCmd)
}

func runTell(cmd *cobra.Command, args []string) error {
	toRole, err := requireNonEmpty("role", args[0])
	if err != nil {
		return err
	}
	content, err := requireNonEmpty("message", args[1])
	if err != nil {
		return err
	}

	d, err := loadDeps(false)
	if err != nil {
		return err
	}
	defer d.Close()

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	m := store.Message{
		FromRole: strings.TrimSpace(tellFrom),
		ToRole:   toRole,
		Content:  content,
		TaskID:   strings.TrimSpace(tellTask),
	}

	if err := d.store.SendMessage(ctx, m); err != nil {
		return fmt.Errorf("send message: %w", err)
	}

	fmt.Printf("✓ Message sent to %s: %s\n", toRole, content)
	return nil
}
