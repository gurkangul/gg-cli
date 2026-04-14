package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/gurkangul/gg-cli/internal/store"
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
	tellCmd.Flags().StringVar(&tellFrom, "from", "", "sender role (defaults to $GG_ROLE, then 'user')")
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
	taskRef, err := normalizeTaskRef(tellTask)
	if err != nil {
		return fmt.Errorf("--task: %w", err)
	}

	from := strings.TrimSpace(tellFrom)
	if from == "" {
		from = strings.TrimSpace(os.Getenv("GG_ROLE"))
	}
	if from == "" {
		from = "user"
	}

	d, err := loadDeps(false)
	if err != nil {
		return err
	}
	defer d.Close()

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	m := store.Message{
		FromRole: from,
		ToRole:   toRole,
		Content:  content,
		TaskID:   taskRef,
	}

	if err := d.store.SendMessage(ctx, m); err != nil {
		return fmt.Errorf("send message: %w", err)
	}

	fmt.Printf("✓ Message sent from %s to %s: %s\n", from, toRole, content)
	return nil
}
