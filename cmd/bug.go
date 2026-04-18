package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/store"
)

var bugCmd = &cobra.Command{
	Use:   "bug",
	Short: "Manage bug lifecycle",
	Long: `Track defects from discovery through fix. Bugs are stored in Qdrant and
searchable by description. Each bug moves through a lifecycle:
  open → fixing → fixed | wontfix → reopened → fixing → fixed`,
}

var validSeverities = map[string]bool{"critical": true, "high": true, "medium": true, "low": true}
var validBugStatuses = map[string]bool{"open": true, "fixing": true, "fixed": true, "wontfix": true, "reopened": true}

func init() {
	rootCmd.AddCommand(bugCmd)
}

func bugStatusIcon(status string) string {
	switch status {
	case "fixed":
		return "✓"
	case "wontfix":
		return "–"
	case "fixing":
		return "→"
	case "reopened":
		return "↺"
	default:
		return "!"
	}
}

func requireBugID(raw string) (string, error) {
	id := strings.ToUpper(strings.TrimSpace(raw))
	if id == "" {
		return "", fmt.Errorf("bug ID is required (expected BUG-NNN)")
	}
	if _, err := store.ParseBugID(id); err != nil {
		return "", err
	}
	return id, nil
}
