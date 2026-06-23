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
	Long: `Track defects from discovery through fix. Bugs are stored in the project
vector store and searchable by description. Each bug moves through a lifecycle:
  open → fixing → fixed | wontfix → reopened → fixing → fixed`,
	// BUG-093: bare `gg bug` and unknown subcommands (e.g. `gg bug show`) used to
	// silently print the help blurb and exit 0, misleading fresh agents. Reject
	// both with a non-zero error instead. `--help` / `gg help bug` still work
	// because cobra handles the help flag before RunE.
	RunE: runBugParent,
}

func runBugParent(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("`gg bug` needs a subcommand — run `gg bug --help` to list them")
	}
	sub := args[0]
	hint := "run `gg bug --help` to list subcommands"
	switch sub {
	case "view", "info", "details":
		hint = "did you mean `gg bug get`?"
	}
	return fmt.Errorf("unknown bug subcommand %q — %s", sub, hint)
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
