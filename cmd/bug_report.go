package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/graph"
	"github.com/gurkangul/gg-cli/internal/store"
)

var bugReportCmd = &cobra.Command{
	Use:   `report "title"`,
	Short: "Report a new bug",
	Args:  cobra.ExactArgs(1),
	RunE:  runBugReport,
}

var (
	bugDetail   string
	bugSeverity string
	bugTags     string
	bugTaskID   string
	bugFiles    string
	bugSymbols  string
)

func init() {
	bugReportCmd.Flags().StringVar(&bugDetail, "detail", "", "detailed description")
	bugReportCmd.Flags().StringVar(&bugSeverity, "severity", "medium", "severity: critical, high, medium, low")
	bugReportCmd.Flags().StringVar(&bugTags, "tags", "", "comma-separated tags")
	bugReportCmd.Flags().StringVar(&bugTaskID, "task", "", "link to a task (e.g. TASK-042)")
	bugReportCmd.Flags().StringVar(&bugFiles, "files", "", "comma-separated source file paths this bug affects")
	bugReportCmd.Flags().StringVar(&bugSymbols, "symbols", "", "comma-separated symbol names this bug affects")
	addFromFlag(bugReportCmd)
	bugCmd.AddCommand(bugReportCmd)
}

// normalizeBugFiles converts raw --files paths to project-relative form.
// Absolute paths are rebased onto the project root; relative paths that escape
// the root are dropped. Paths that can't be normalized (root detection failed)
// are passed through unchanged so the caller still gets useful output.
func normalizeBugFiles(paths []string) []string {
	root, err := config.FindRoot()
	if err != nil {
		return paths
	}
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if rel, ok := normalizeProjectPath(root, "", p); ok {
			out = append(out, rel)
		}
		// paths outside the project root are silently dropped
	}
	return out
}

func runBugReport(cmd *cobra.Command, args []string) error {
	printProjectBanner()
	title, err := requireNonEmpty("title", args[0])
	if err != nil {
		return err
	}
	if !validSeverities[bugSeverity] {
		return fmt.Errorf("invalid severity %q — use critical, high, medium, or low", bugSeverity)
	}
	taskID, err := normalizeTaskRef(bugTaskID)
	if err != nil {
		return err
	}

	if err := requireAgentIdentity(); err != nil {
		return err
	}

	d, err := loadDeps(true)
	if err != nil {
		return err
	}
	defer d.Close()

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	if err := runInboxGatePreflight(ctx, d.store, "bug-report"); err != nil {
		return err
	}

	embedText := title
	if bugDetail != "" {
		embedText = title + " " + bugDetail
	}
	vector, err := d.embedder.Generate(ctx, embedText)
	if err != nil {
		return fmt.Errorf("generate embedding: %w", err)
	}

	if promptIfDuplicate(ctx, d, "bugs", vector) {
		fmt.Println("Aborted — no bug reported.")
		return nil
	}

	affectedFiles := normalizeBugFiles(parseTags(bugFiles))
	affectedSymbols := parseTags(bugSymbols)

	b := store.Bug{
		Title:           title,
		Detail:          strings.TrimSpace(bugDetail),
		Severity:        bugSeverity,
		Tags:            parseTags(bugTags),
		TaskID:          taskID,
		AffectedFiles:   affectedFiles,
		AffectedSymbols: affectedSymbols,
		By:              resolveAuthor(cmd),
	}

	id, err := d.store.ReportBug(ctx, b, vector)
	if err != nil {
		return fmt.Errorf("report bug: %w", err)
	}

	// Write Bug node + AFFECTS edges to Memgraph when configured. Always
	// upsert the Bug node, even when no affected files/symbols were given —
	// otherwise `gg impact BUG-NNN` cannot find the bug and onelift/qrmenu
	// end up with 0 Bug nodes in Memgraph despite a populated Qdrant.
	if cfg, cfgErr := config.Load(); cfgErr == nil && cfg != nil && cfg.Memgraph.URI != "" {
		if gc, gcErr := graph.New(&cfg.Memgraph, cfg.ProjectID); gcErr == nil {
			gctx, gcancel := withTimeout(cmd.Context())
			defer gcancel()
			if mergeErr := gc.MergeBugAffects(gctx, id, title, affectedFiles, affectedSymbols); mergeErr != nil {
				fmt.Printf("~ graph edges skipped: %v\n", mergeErr)
			}
			_ = gc.Close(gctx)
		}
	}

	return printJSON(map[string]any{"id": id, "title": title, "severity": bugSeverity}, func() {
		fmt.Printf("bug reported: %s — %s [%s]\n", id, title, bugSeverity)
	})
}
