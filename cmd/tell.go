package cmd

import (
	"context"
	"fmt"

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
	toRole := args[0]
	content := args[1]

	embedder, err := newEmbedder()
	if err != nil {
		return err
	}
	vector, err := embedder.Generate(content)
	if err != nil {
		return fmt.Errorf("generate embedding: %w", err)
	}

	client, err := newStoreClient()
	if err != nil {
		return err
	}
	defer client.Close()

	m := store.Message{
		FromRole: tellFrom,
		ToRole:   toRole,
		Content:  content,
		TaskID:   tellTask,
	}

	if err := client.SendMessage(context.Background(), m, vector); err != nil {
		return fmt.Errorf("send message: %w", err)
	}

	fmt.Printf("✓ Message sent to %s: %s\n", toRole, content)
	return nil
}
