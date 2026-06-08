package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/store"
)

var onboardCmd = &cobra.Command{
	Use:   "onboard",
	Short: "Print what a new agent inherits — the distilled project briefing + how to work here",
	Long: `gg onboard prints the briefing a fresh agent should read to work like a senior
dev on this project: the auto-derived canon (key decisions, rejected approaches,
known failure modes), the invariants, and the core gg workflow — plus the token
cost of the briefing. It is the "30-second 10-year-dev" proof (TASK-478).`,
	RunE: runOnboard,
}

func init() {
	rootCmd.AddCommand(onboardCmd)
}

// formatOnboarding assembles the newcomer briefing from the distilled canon plus
// the durable invariants and workflow. Pure so it is unit-testable.
func formatOnboarding(canon []store.CanonEntry) string {
	var b strings.Builder
	b.WriteString("=== gg ONBOARDING — what a senior dev here already knows ===\n\n")
	if len(canon) == 0 {
		b.WriteString("(no distilled knowledge yet — record decisions and the canon builds itself)\n\n")
	}
	for _, e := range canon {
		b.WriteString("## " + e.Area + "\n" + e.Text + "\n\n")
	}
	b.WriteString("## how to work here\n")
	b.WriteString("- Orient:    gg session-start --agent <id> --role <role>\n")
	b.WriteString("- Recall:    gg search \"<question>\"   |   gg context --for-task TASK-N\n")
	b.WriteString("- Record:    gg record \"<decision>\" --reason \"…\"   |   gg bug report …\n")
	b.WriteString("- Lifecycle: gg task create -> start -> ready-for-live -> done (gates make done = verified)\n")
	b.WriteString("- Invariants: JSONL is the source of truth; no-network / no-daemon; never bypass gates.\n")
	return b.String()
}

func runOnboard(cmd *cobra.Command, _ []string) error {
	d, err := loadDepsReadOnly(false)
	if err != nil {
		return err
	}
	defer d.Close()

	var canon []store.CanonEntry
	if !d.qdrantDown {
		ctx, cancel := withTimeout(cmd.Context())
		defer cancel()
		decs, _ := d.store.ListDecisions(ctx, 0, false)
		rejs, _ := d.store.ListRejections(ctx, 0)
		bugs, _ := d.store.ListBugs(ctx, "fixed")
		tasks, _ := d.store.ListTasks(ctx, "")
		canon = store.BuildAutoCanon(decs, rejs, bugs, tasks)
	}

	out := formatOnboarding(canon)
	w := cmd.OutOrStdout()
	fmt.Fprint(w, out)
	fmt.Fprintf(w, "\n— briefing: %d bytes ≈ %d tokens (one read; recall the rest on demand) —\n", len(out), len(out)/4)
	return nil
}
