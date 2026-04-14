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
}

var validPriorities = map[string]bool{"high": true, "medium": true, "low": true}
var validStatuses = map[string]bool{"pending": true, "in_progress": true, "done": true, "blocked": true}

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
	default:
		return "○"
	}
}
