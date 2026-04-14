package cmd

import (
	"fmt"
	"strings"
	"sync"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/store"
)

var contextCmd = &cobra.Command{
	Use:   `context "topic"`,
	Short: "Fetch a unified context bundle for a topic",
	Long: `Searches decisions, rejections, tasks, and discussions for the given topic
using semantic similarity and returns a bundled context package for agent consumption.

Phase 2 will add Memgraph structural queries (affected files/symbols).`,
	Args: cobra.ExactArgs(1),
	RunE: runContext,
}

var contextLimit uint64
var contextIncludeResolved bool
var contextFullTranscript bool

func init() {
	contextCmd.Flags().Uint64Var(&contextLimit, "limit", 5, "max results per collection")
	contextCmd.Flags().BoolVar(&contextIncludeResolved, "include-resolved", false, "include resolved/dismissed discussions and done/blocked tasks")
	contextCmd.Flags().BoolVar(&contextFullTranscript, "full", false, "print full deliberation transcript for each discussion")
	rootCmd.AddCommand(contextCmd)
}

type contextBundle struct {
	decisions   []store.Decision
	rejections  []store.Rejection
	tasks       []store.Task
	discussions []store.Discussion
	notes       []store.Note
	decErr      error
	rejErr      error
	taskErr     error
	discErr     error
	noteErr     error
}

func runContext(cmd *cobra.Command, args []string) error {
	query, err := requireNonEmpty("topic", args[0])
	if err != nil {
		return err
	}

	d, err := loadDepsReadOnly(true)
	if err != nil {
		return err
	}
	defer d.Close()

	if d.qdrantDown {
		fmt.Fprintln(cmd.OutOrStderr(), "⚠ Qdrant unreachable — read-only fallback mode (context bundle unavailable)")
		return nil
	}

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	vector, err := d.embedder.Generate(ctx, query)
	if err != nil {
		return fmt.Errorf("generate embedding: %w", err)
	}

	// Run all searches in parallel.
	var bundle contextBundle
	var wg sync.WaitGroup
	wg.Add(5)

	go func() {
		defer wg.Done()
		bundle.decisions, bundle.decErr = d.store.SearchDecisions(ctx, vector, contextLimit)
	}()
	go func() {
		defer wg.Done()
		bundle.rejections, bundle.rejErr = d.store.SearchRejections(ctx, vector, contextLimit)
	}()
	go func() {
		defer wg.Done()
		bundle.tasks, bundle.taskErr = d.store.SearchTasks(ctx, vector, contextLimit, contextIncludeResolved)
	}()
	go func() {
		defer wg.Done()
		bundle.discussions, bundle.discErr = d.store.SearchDiscussions(ctx, vector, contextLimit, contextIncludeResolved)
	}()
	go func() {
		defer wg.Done()
		bundle.notes, bundle.noteErr = d.store.SearchNotes(ctx, vector, contextLimit)
	}()

	wg.Wait()

	// Collect any errors as warnings — partial results are still useful.
	var errs []string
	if bundle.decErr != nil {
		errs = append(errs, fmt.Sprintf("decisions: %v", bundle.decErr))
	}
	if bundle.rejErr != nil {
		errs = append(errs, fmt.Sprintf("rejections: %v", bundle.rejErr))
	}
	if bundle.taskErr != nil {
		errs = append(errs, fmt.Sprintf("tasks: %v", bundle.taskErr))
	}
	if bundle.discErr != nil {
		errs = append(errs, fmt.Sprintf("discussions: %v", bundle.discErr))
	}
	if bundle.noteErr != nil {
		errs = append(errs, fmt.Sprintf("notes: %v", bundle.noteErr))
	}

	total := len(bundle.decisions) + len(bundle.rejections) + len(bundle.tasks) + len(bundle.discussions) + len(bundle.notes)
	if total == 0 && len(errs) > 0 {
		return fmt.Errorf("all searches failed:\n  %s", strings.Join(errs, "\n  "))
	}

	jsonPayload := map[string]any{
		"query":       query,
		"decisions":   bundle.decisions,
		"rejections":  bundle.rejections,
		"tasks":       bundle.tasks,
		"discussions": bundle.discussions,
		"notes":       bundle.notes,
		"warnings":    errs,
	}

	return printJSON(jsonPayload, func() {
		fmt.Printf("CONTEXT BUNDLE: %q\n", query)
		fmt.Println(strings.Repeat("─", 60))

	if len(bundle.decisions) > 0 {
		fmt.Println("\nDECISIONS:")
		for _, dec := range bundle.decisions {
			fmt.Printf("  • [%s] %s\n", shortDate(dec.CreatedAt), dec.Text)
			if dec.Reason != "" {
				fmt.Printf("    Reason: %s\n", dec.Reason)
			}
			if len(dec.Tags) > 0 {
				fmt.Printf("    Tags: %s\n", strings.Join(dec.Tags, ", "))
			}
			if dec.TaskID != "" {
				fmt.Printf("    Task: %s\n", dec.TaskID)
			}
		}
	}

	if len(bundle.rejections) > 0 {
		fmt.Println("\nREJECTIONS:")
		for _, r := range bundle.rejections {
			fmt.Printf("  ✗ [%s] %s\n", shortDate(r.CreatedAt), r.Approach)
			if r.Reason != "" {
				fmt.Printf("    Reason: %s\n", r.Reason)
			}
			if len(r.Tags) > 0 {
				fmt.Printf("    Tags: %s\n", strings.Join(r.Tags, ", "))
			}
			if r.TaskID != "" {
				fmt.Printf("    Task: %s\n", r.TaskID)
			}
		}
	}

	if len(bundle.tasks) > 0 {
		fmt.Println("\nTASKS:")
		for _, t := range bundle.tasks {
			statusIcon := taskStatusIcon(t.Status)
			fmt.Printf("  %s [%s] %s — %s\n", statusIcon, t.ID, t.Title, t.Priority)
			if t.Detail != "" {
				detail := t.Detail
				if len(detail) > 120 {
					detail = detail[:117] + "..."
				}
				fmt.Printf("    %s\n", detail)
			}
		}
	}

	if len(bundle.discussions) > 0 {
		fmt.Println("\nDISCUSSIONS:")
		for _, disc := range bundle.discussions {
			statusMark := discStatusMark(disc.Status)
			fmt.Printf("  %s [%s] %s\n", statusMark, disc.ID, disc.Topic)
			if disc.Detail != "" {
				detail := disc.Detail
				if len(detail) > 120 {
					detail = detail[:117] + "..."
				}
				fmt.Printf("    %s\n", detail)
			}
			if disc.Status == "resolved" && disc.ResolvedNote != "" {
				fmt.Printf("    Resolved: %s\n", disc.ResolvedNote)
			}
			if contextFullTranscript && len(disc.Turns) > 0 {
				fmt.Printf("    Transcript (%d turns):\n", len(disc.Turns))
				for i, t := range disc.Turns {
					fmt.Printf("      [%d] %s (%s): %s\n", i+1, t.By, t.Role, t.Text)
				}
			} else if len(disc.Turns) > 0 {
				last := disc.Turns[len(disc.Turns)-1]
				fmt.Printf("    Latest: %s (%s) — %s\n", last.By, last.Role, last.Text)
				if len(disc.Turns) > 1 {
					fmt.Printf("    (%d more turns — use --full to show all)\n", len(disc.Turns)-1)
				}
			}
		}
	}

	if len(bundle.notes) > 0 {
		fmt.Println("\nNOTES:")
		for _, n := range bundle.notes {
			fmt.Printf("  [%s]", shortDate(n.CreatedAt))
			if n.TaskID != "" {
				fmt.Printf(" (%s)", n.TaskID)
			}
			fmt.Printf(" %s\n", n.Text)
		}
	}

	fmt.Println()
	fmt.Printf("  %d decisions  %d rejections  %d tasks  %d discussions  %d notes\n",
		len(bundle.decisions), len(bundle.rejections), len(bundle.tasks), len(bundle.discussions), len(bundle.notes))

	// TODO(Phase 2): add Memgraph structural query — affected files and symbols
	// related to the query topic via graph traversal.

		if len(errs) > 0 {
			fmt.Printf("\nWarnings:\n  %s\n", strings.Join(errs, "\n  "))
		}
	})
}

// shortDate returns the first 10 characters of an RFC3339 timestamp (YYYY-MM-DD).
// Returns "—" for empty or short strings to avoid panics.
func shortDate(ts string) string {
	if len(ts) >= 10 {
		return ts[:10]
	}
	return "—"
}

func taskStatusIcon(status string) string {
	switch status {
	case "done":
		return "✓"
	case "in_progress":
		return "→"
	case "blocked":
		return "!"
	default:
		return "○"
	}
}

func discStatusMark(status string) string {
	switch status {
	case "resolved":
		return "✓"
	case "dismissed":
		return "–"
	default:
		return "?"
	}
}
