package cmd

import (
	"fmt"
	"strings"
	"sync"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg/internal/store"
)

var bugCmd = &cobra.Command{
	Use:   "bug",
	Short: "Manage bug lifecycle",
	Long: `Track defects from discovery through fix. Bugs are stored in Qdrant and
searchable by description. Each bug moves through a lifecycle:
  open → fixing → fixed | wontfix`,
}

var validSeverities = map[string]bool{"critical": true, "high": true, "medium": true, "low": true}
var validBugStatuses = map[string]bool{"open": true, "fixing": true, "fixed": true, "wontfix": true}

// --- bug report ---

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

// --- bug list ---

var bugListCmd = &cobra.Command{
	Use:   "list",
	Short: "List bugs",
	Args:  cobra.NoArgs,
	RunE:  runBugList,
}

var bugListStatus string

// --- bug get ---

var bugGetCmd = &cobra.Command{
	Use:   "get BUG-ID",
	Short: "Show bug details",
	Args:  cobra.ExactArgs(1),
	RunE:  runBugGet,
}

// --- bug fix ---

var bugFixCmd = &cobra.Command{
	Use:   `fix BUG-ID "summary"`,
	Short: "Mark a bug as fixed",
	Args:  cobra.ExactArgs(2),
	RunE:  runBugFix,
}

var bugRootCause string

// --- bug start ---

var bugStartCmd = &cobra.Command{
	Use:   "start BUG-ID",
	Short: "Move a bug to 'fixing' status",
	Args:  cobra.ExactArgs(1),
	RunE:  runBugStart,
}

// --- bug wontfix ---

var bugWontFixCmd = &cobra.Command{
	Use:   `wontfix BUG-ID "reason"`,
	Short: "Close a bug as won't-fix",
	Args:  cobra.ExactArgs(2),
	RunE:  runBugWontFix,
}

// --- bug triage (TASK-025) ---

var bugTriageCmd = &cobra.Command{
	Use:   "triage BUG-ID",
	Short: "Auto context bundle for fixing a bug",
	Long: `Fetches the bug, then runs a parallel semantic search across all collections
(decisions, rejections, tasks, discussions, notes) using the bug title as the
query. The result is a bundled context package to prime an agent's fix.`,
	Args: cobra.ExactArgs(1),
	RunE: runBugTriage,
}

var bugTriageLimit uint64

func init() {
	bugReportCmd.Flags().StringVar(&bugDetail, "detail", "", "detailed description")
	bugReportCmd.Flags().StringVar(&bugSeverity, "severity", "medium", "severity: critical, high, medium, low")
	bugReportCmd.Flags().StringVar(&bugTags, "tags", "", "comma-separated tags")
	bugReportCmd.Flags().StringVar(&bugTaskID, "task", "", "link to a task (e.g. TASK-042)")
	bugListCmd.Flags().StringVar(&bugListStatus, "status", "", "filter by status: open, fixing, fixed, wontfix")
	bugFixCmd.Flags().StringVar(&bugRootCause, "root-cause", "", "root cause identified during fix")
	bugTriageCmd.Flags().Uint64Var(&bugTriageLimit, "limit", 5, "max results per collection")

	bugCmd.AddCommand(bugReportCmd)
	bugCmd.AddCommand(bugListCmd)
	bugCmd.AddCommand(bugGetCmd)
	bugCmd.AddCommand(bugFixCmd)
	bugCmd.AddCommand(bugStartCmd)
	bugCmd.AddCommand(bugWontFixCmd)
	bugCmd.AddCommand(bugTriageCmd)
	rootCmd.AddCommand(bugCmd)
}

func runBugReport(cmd *cobra.Command, args []string) error {
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

func runBugList(cmd *cobra.Command, _ []string) error {
	if bugListStatus != "" && !validBugStatuses[bugListStatus] {
		return fmt.Errorf("invalid status %q — use open, fixing, fixed, or wontfix", bugListStatus)
	}

	d, err := loadDeps(false)
	if err != nil {
		return err
	}
	defer d.Close()

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	bugs, err := d.store.ListBugs(ctx, bugListStatus)
	if err != nil {
		return fmt.Errorf("list bugs: %w", err)
	}

	return printJSON(bugs, func() {
		if len(bugs) == 0 {
			fmt.Println("No bugs found.")
			return
		}
		for _, b := range bugs {
			fmt.Printf("%s %s [%s/%s] %s\n", bugStatusIcon(b.Status), b.ID, b.Severity, b.Status, b.Title)
			if b.Status == "fixed" && b.FixSummary != "" {
				fmt.Printf("    ✓ Fix: %s\n", b.FixSummary)
			}
		}
	})
}

func runBugGet(cmd *cobra.Command, args []string) error {
	bugID, err := requireBugID(args[0])
	if err != nil {
		return err
	}

	d, err := loadDeps(false)
	if err != nil {
		return err
	}
	defer d.Close()

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	b, err := d.store.GetBug(ctx, bugID)
	if err != nil {
		return notFound(err.Error())
	}

	return printJSON(b, func() {
		fmt.Printf("%s %s [%s/%s] %s\n", bugStatusIcon(b.Status), b.ID, b.Severity, b.Status, b.Title)
		if b.Detail != "" {
			fmt.Printf("  Detail: %s\n", b.Detail)
		}
		if len(b.Tags) > 0 {
			fmt.Printf("  Tags: %s\n", strings.Join(b.Tags, ", "))
		}
		if b.TaskID != "" {
			fmt.Printf("  Task: %s\n", b.TaskID)
		}
		if b.RootCause != "" {
			fmt.Printf("  Root cause: %s\n", b.RootCause)
		}
		if b.FixSummary != "" {
			fmt.Printf("  Fix: %s\n", b.FixSummary)
		}
		fmt.Printf("  Created: %s\n", shortDate(b.CreatedAt))
		fmt.Printf("  Updated: %s\n", shortDate(b.UpdatedAt))
	})
}

func runBugFix(cmd *cobra.Command, args []string) error {
	bugID, err := requireBugID(args[0])
	if err != nil {
		return err
	}
	summary, err := requireNonEmpty("summary", args[1])
	if err != nil {
		return err
	}
	rootCause, err := requireNonEmpty("--root-cause", bugRootCause)
	if err != nil {
		return fmt.Errorf("%w (root cause must be the underlying defect, not the symptom — required to close a bug)", err)
	}

	d, err := loadDeps(false)
	if err != nil {
		return err
	}
	defer d.Close()

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	if err := d.store.FixBug(ctx, bugID, rootCause, summary); err != nil {
		return err
	}

	return printJSON(map[string]any{"id": bugID, "status": "fixed", "summary": summary, "root_cause": rootCause}, func() {
		fmt.Printf("✓ %s marked as fixed\n", bugID)
		fmt.Printf("  Root cause: %s\n", rootCause)
	})
}

func runBugStart(cmd *cobra.Command, args []string) error {
	bugID, err := requireBugID(args[0])
	if err != nil {
		return err
	}

	d, err := loadDeps(false)
	if err != nil {
		return err
	}
	defer d.Close()

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	if err := d.store.StartFixingBug(ctx, bugID); err != nil {
		return err
	}

	return printJSON(map[string]any{"id": bugID, "status": "fixing"}, func() {
		fmt.Printf("→ %s marked as fixing\n", bugID)
	})
}

func runBugWontFix(cmd *cobra.Command, args []string) error {
	bugID, err := requireBugID(args[0])
	if err != nil {
		return err
	}
	reason, err := requireNonEmpty("reason", args[1])
	if err != nil {
		return err
	}

	d, err := loadDeps(false)
	if err != nil {
		return err
	}
	defer d.Close()

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	if err := d.store.WontFixBug(ctx, bugID, reason); err != nil {
		return err
	}

	return printJSON(map[string]any{"id": bugID, "status": "wontfix", "reason": reason}, func() {
		fmt.Printf("– %s marked as wontfix\n", bugID)
	})
}

// runBugTriage implements TASK-025: auto context bundle for a bug fix.
// It fetches the bug, then runs parallel semantic search across all collections.
func runBugTriage(cmd *cobra.Command, args []string) error {
	bugID, err := requireBugID(args[0])
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

	b, err := d.store.GetBug(ctx, bugID)
	if err != nil {
		return err
	}

	query := b.Title
	if b.Detail != "" {
		query = b.Title + " " + b.Detail
	}

	vector, err := d.embedder.Generate(ctx, query)
	if err != nil {
		return fmt.Errorf("generate embedding: %w", err)
	}

	// Parallel search across all collections.
	var (
		decisions   []store.Decision
		rejections  []store.Rejection
		tasks       []store.Task
		discussions []store.Discussion
		notes       []store.Note
		decErr, rejErr, taskErr, discErr, noteErr error
	)

	var wg sync.WaitGroup
	wg.Add(5)
	go func() { defer wg.Done(); decisions, decErr = d.store.SearchDecisions(ctx, vector, bugTriageLimit) }()
	go func() { defer wg.Done(); rejections, rejErr = d.store.SearchRejections(ctx, vector, bugTriageLimit) }()
	go func() { defer wg.Done(); tasks, taskErr = d.store.SearchTasks(ctx, vector, bugTriageLimit) }()
	go func() { defer wg.Done(); discussions, discErr = d.store.SearchDiscussions(ctx, vector, bugTriageLimit) }()
	go func() { defer wg.Done(); notes, noteErr = d.store.SearchNotes(ctx, vector, bugTriageLimit) }()
	wg.Wait()

	// Collect warnings from parallel search errors.
	var warnings []string
	for _, e := range []error{decErr, rejErr, taskErr, discErr, noteErr} {
		if e != nil {
			warnings = append(warnings, e.Error())
		}
	}

	payload := map[string]any{
		"bug":         b,
		"decisions":   decisions,
		"rejections":  rejections,
		"tasks":       tasks,
		"discussions": discussions,
		"notes":       notes,
		"warnings":    warnings,
	}

	return printJSON(payload, func() {
		fmt.Printf("BUG TRIAGE: %s — %s [%s/%s]\n", b.ID, b.Title, b.Severity, b.Status)
		fmt.Println(strings.Repeat("─", 60))
		if b.Detail != "" {
			fmt.Printf("Detail: %s\n", b.Detail)
		}

		if len(decisions) > 0 {
			fmt.Println("\nRELATED DECISIONS:")
			for _, dec := range decisions {
				fmt.Printf("  • [%s] %s\n", shortDate(dec.CreatedAt), dec.Text)
				if dec.Reason != "" {
					fmt.Printf("    Reason: %s\n", dec.Reason)
				}
			}
		}

		if len(rejections) > 0 {
			fmt.Println("\nRELATED REJECTIONS:")
			for _, r := range rejections {
				fmt.Printf("  ✗ [%s] %s\n", shortDate(r.CreatedAt), r.Approach)
				if r.Reason != "" {
					fmt.Printf("    Reason: %s\n", r.Reason)
				}
			}
		}

		if len(tasks) > 0 {
			fmt.Println("\nRELATED TASKS:")
			for _, t := range tasks {
				fmt.Printf("  %s [%s] %s — %s\n", taskStatusIcon(t.Status), t.ID, t.Title, t.Priority)
			}
		}

		if len(discussions) > 0 {
			fmt.Println("\nRELATED DISCUSSIONS:")
			for _, disc := range discussions {
				fmt.Printf("  %s [%s] %s\n", discStatusMark(disc.Status), disc.ID, disc.Topic)
			}
		}

		if len(notes) > 0 {
			fmt.Println("\nRELATED NOTES:")
			for _, n := range notes {
				fmt.Printf("  [%s] %s\n", shortDate(n.CreatedAt), n.Text)
			}
		}

		if len(warnings) > 0 {
			fmt.Printf("\nWarnings: %s\n", strings.Join(warnings, "; "))
		}

		fmt.Printf("\n  %d decisions  %d rejections  %d tasks  %d discussions  %d notes\n",
			len(decisions), len(rejections), len(tasks), len(discussions), len(notes))
	})
}

func bugStatusIcon(status string) string {
	switch status {
	case "fixed":
		return "✓"
	case "wontfix":
		return "–"
	case "fixing":
		return "→"
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
