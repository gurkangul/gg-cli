package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/store"
)

var noteCmd = &cobra.Command{
	Use:   `note "text"`,
	Short: "Record a free-form note (deprecated — use gg record or commit message)",
	Long: `Stores a timestamped note in the activity log.

DEPRECATED: usage data shows note is underused (1 call in dogfood).
  - For decisions and rationale: use 'gg record "text" --reason "why"'
  - For progress context: use a git commit message or 'gg task done TASK-X "summary"'
  - For search: 'gg search' covers decisions, tasks, and messages

This command will be removed in a future release (v0.2+).
TODO(v0.2): remove gg note`,
	Args: cobra.ExactArgs(1),
	RunE: runNote,
}

var noteTags string
var noteTaskID string

var noteListCmd = &cobra.Command{
	Use:   "list",
	Short: "List recent notes",
	Args:  cobra.NoArgs,
	RunE:  runNoteList,
}

var noteListLimit int

var noteSearchCmd = &cobra.Command{
	Use:   `search "query"`,
	Short: "Semantic search over notes",
	Args:  cobra.ExactArgs(1),
	RunE:  runNoteSearch,
}

var noteSearchLimit uint64

func init() {
	noteCmd.Flags().StringVar(&noteTags, "tags", "", "comma-separated tags")
	noteCmd.Flags().StringVar(&noteTaskID, "task", "", "link note to a task (e.g. TASK-042)")
	noteListCmd.Flags().IntVar(&noteListLimit, "limit", 20, "max notes to show")
	noteSearchCmd.Flags().Uint64Var(&noteSearchLimit, "limit", 5, "max results to return")
	noteCmd.AddCommand(noteListCmd)
	noteCmd.AddCommand(noteSearchCmd)
	rootCmd.AddCommand(noteCmd)
}

func runNote(cmd *cobra.Command, args []string) error {
	fmt.Fprintln(cmd.ErrOrStderr(), "warning: 'gg note' is deprecated — use 'gg record' for decisions or 'gg task done' for progress notes")
	printProjectBanner()
	text, err := requireNonEmpty("text", args[0])
	if err != nil {
		return err
	}

	taskID, err := normalizeTaskRef(noteTaskID)
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

	vector, err := d.embedder.Generate(ctx, text)
	if err != nil {
		return fmt.Errorf("generate embedding: %w", err)
	}

	if promptIfDuplicate(ctx, d, "notes", vector) {
		fmt.Println("Aborted — no note saved.")
		return nil
	}

	n := store.Note{
		Text:   text,
		Tags:   parseTags(noteTags),
		TaskID: taskID,
	}

	id, err := d.store.AddNote(ctx, n, vector)
	if err != nil {
		return fmt.Errorf("save note: %w", err)
	}

	return printJSON(map[string]any{"id": id, "text": n.Text, "tags": n.Tags, "task_id": n.TaskID}, func() {
		fmt.Printf("note saved (%s)\n", id[:8])
	})
}

func runNoteList(cmd *cobra.Command, _ []string) error {
	d, err := loadDeps(false)
	if err != nil {
		return err
	}
	defer d.Close()

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	notes, err := d.store.ListNotes(ctx, noteListLimit)
	if err != nil {
		return fmt.Errorf("list notes: %w", err)
	}

	return printJSON(notes, func() {
		if len(notes) == 0 {
			fmt.Println("No notes yet. Add one with: gg note \"your observation\"")
			return
		}
		for _, n := range notes {
			fmt.Printf("[%s]", shortDate(n.CreatedAt))
			if n.TaskID != "" {
				fmt.Printf(" (%s)", n.TaskID)
			}
			if len(n.Tags) > 0 {
				fmt.Printf(" #%s", strings.Join(n.Tags, " #"))
			}
			fmt.Printf("\n  %s\n", n.Text)
		}
	})
}

func runNoteSearch(cmd *cobra.Command, args []string) error {
	query, err := requireNonEmpty("query", args[0])
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

	vector, err := d.embedder.Generate(ctx, query)
	if err != nil {
		return fmt.Errorf("generate embedding: %w", err)
	}

	notes, err := d.store.SearchNotes(ctx, vector, noteSearchLimit)
	if err != nil {
		return fmt.Errorf("search notes: %w", err)
	}

	return printJSON(notes, func() {
		if len(notes) == 0 {
			fmt.Println("No matching notes.")
			return
		}
		for _, n := range notes {
			fmt.Printf("[%s]", shortDate(n.CreatedAt))
			if n.TaskID != "" {
				fmt.Printf(" (%s)", n.TaskID)
			}
			if len(n.Tags) > 0 {
				fmt.Printf(" #%s", strings.Join(n.Tags, " #"))
			}
			fmt.Printf("\n  %s\n", n.Text)
		}
	})
}
