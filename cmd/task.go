package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/store"
)

var taskCmd = &cobra.Command{
	Use:   "task",
	Short: "Manage tasks",
	// BUG-093(d): an unknown subcommand (e.g. `gg task show`, `gg task frob`)
	// used to silently print the help blurb. Reject it with a suggestion
	// instead. Bare `gg task` (no args) still prints help.
	RunE: runTaskParent,
}

func runTaskParent(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return cmd.Help()
	}
	sub := args[0]
	hint := "run `gg task --help` to list subcommands"
	switch sub {
	case "show", "view", "info", "details":
		hint = "did you mean `gg task get`?"
	}
	return fmt.Errorf("unknown task subcommand %q — %s", sub, hint)
}

var validPriorities = map[string]bool{"high": true, "medium": true, "low": true}
var validStatuses = map[string]bool{"pending": true, "in_progress": true, "ready_for_live": true, "done": true, "blocked": true}

func init() {
	rootCmd.AddCommand(taskCmd)
}

// parseTaskIDList parses a comma-separated list of task IDs, validating each.
func parseTaskIDList(raw string) ([]string, error) {
	if raw == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	ids := make([]string, 0, len(parts))
	for _, p := range parts {
		id := strings.ToUpper(strings.TrimSpace(p))
		if id == "" {
			continue
		}
		if _, err := store.ParseTaskID(id); err != nil {
			return nil, fmt.Errorf("invalid task ID %q: %w", p, err)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func statusIcon(status string) string {
	switch status {
	case "done":
		return "✓"
	case "blocked":
		return "⚠"
	case "in_progress":
		return "→"
	case "ready_for_live":
		return "◉"
	default:
		return "○"
	}
}
