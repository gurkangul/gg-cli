package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

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
)

func init() {
	bugReportCmd.Flags().StringVar(&bugDetail, "detail", "", "detailed description")
	bugReportCmd.Flags().StringVar(&bugSeverity, "severity", "medium", "severity: critical, high, medium, low")
	bugReportCmd.Flags().StringVar(&bugTags, "tags", "", "comma-separated tags")
	bugReportCmd.Flags().StringVar(&bugTaskID, "task", "", "link to a task (e.g. TASK-042)")
	bugCmd.AddCommand(bugReportCmd)
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

	d, err := loadDeps(true)
	if err != nil {
		return err
	}
	defer d.Close()

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

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

	b := store.Bug{
		Title:    title,
		Detail:   strings.TrimSpace(bugDetail),
		Severity: bugSeverity,
		Tags:     parseTags(bugTags),
		TaskID:   taskID,
	}

	id, err := d.store.ReportBug(ctx, b, vector)
	if err != nil {
		return fmt.Errorf("report bug: %w", err)
	}

	return printJSON(map[string]any{"id": id, "title": title, "severity": bugSeverity}, func() {
		fmt.Printf("bug reported: %s — %s [%s]\n", id, title, bugSeverity)
	})
}
