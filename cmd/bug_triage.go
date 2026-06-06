package cmd

import (
	"fmt"
	"strings"
	"sync"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/store"
)

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
	bugTriageCmd.Flags().Uint64Var(&bugTriageLimit, "limit", 5, "max results per collection")
	bugCmd.AddCommand(bugTriageCmd)
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
		decisions                                 []store.Decision
		rejections                                []store.Rejection
		tasks                                     []store.Task
		discussions                               []store.Discussion
		notes                                     []store.Note
		decErr, rejErr, taskErr, discErr, noteErr error
	)

	var wg sync.WaitGroup
	wg.Add(5)
	go func() { defer wg.Done(); decisions, decErr = d.store.SearchDecisions(ctx, vector, bugTriageLimit, false) }()
	go func() { defer wg.Done(); rejections, rejErr = d.store.SearchRejections(ctx, vector, bugTriageLimit) }()
	go func() { defer wg.Done(); tasks, taskErr = d.store.SearchTasks(ctx, vector, bugTriageLimit, true) }()
	go func() {
		defer wg.Done()
		discussions, discErr = d.store.SearchDiscussions(ctx, vector, bugTriageLimit, true)
	}()
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
	recordBugFullHydration(b.ID)

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
