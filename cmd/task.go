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
	// BUG-093: an unknown subcommand (e.g. `gg task frob`) and bare `gg task`
	// used to silently print the help blurb and exit 0, misleading fresh agents.
	// Reject both with a non-zero error. `--help` / `gg help task` still work
	// because cobra handles the help flag before RunE. (`gg task show` now routes
	// to the `show`→`get` alias, so it no longer reaches this path.)
	RunE: runTaskParent,
}

func runTaskParent(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("`gg task` needs a subcommand — run `gg task --help` to list them")
	}
	sub := args[0]
	hint := "run `gg task --help` to list subcommands"
	switch sub {
	case "view", "info", "details":
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
