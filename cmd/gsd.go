package cmd

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	_ "modernc.org/sqlite" // registers "sqlite" driver
)

var gsdCmd = &cobra.Command{
	Use:   "gsd",
	Short: "GSD integration utilities",
}

var gsdAuditCmd = &cobra.Command{
	Use:   "audit",
	Short: "Audit GSD scratchpad items against gg durable work",
	Long: `Scans .gsd/gsd.db for T-level tasks and compares them against gg tasks
tagged "gsd". GSD is allowed as a local scratchpad/helper; this audit is
advisory and highlights GSD tasks that may need a durable gg task.

Exit codes:
  0  audit completed
`,
	RunE: runGSDaudit,
}

var gsdAuditProject string

func init() {
	gsdAuditCmd.Flags().StringVar(&gsdAuditProject, "project", ".", "path to project root (default: current directory)")
	gsdCmd.AddCommand(gsdAuditCmd)
	rootCmd.AddCommand(gsdCmd)
}

type gsdAuditTask struct {
	MilestoneID string
	SliceID     string
	TaskID      string
	Title       string
	Status      string
}

// expectedTitlePrefix returns the canonical [GSD:...] prefix for a GSD task.
func (t gsdAuditTask) expectedTitlePrefix() string {
	return fmt.Sprintf("[GSD:%s-%s-%s]", t.MilestoneID, t.SliceID, t.TaskID)
}

func runGSDaudit(cmd *cobra.Command, _ []string) error {
	projectDir, err := filepath.Abs(gsdAuditProject)
	if err != nil {
		return fmt.Errorf("resolve project path: %w", err)
	}

	dbPath := filepath.Join(projectDir, ".gsd", "gsd.db")
	if _, err := os.Stat(dbPath); err != nil {
		return fmt.Errorf("no GSD database at %s — run from a GSD project root or use --project", dbPath)
	}

	gsdTasks, err := readGSDTasks(cmd.Context(), dbPath)
	if err != nil {
		return fmt.Errorf("read gsd.db: %w", err)
	}

	if len(gsdTasks) == 0 {
		fmt.Println("No GSD tasks found.")
		return nil
	}

	d, err := loadDeps(false)
	if err != nil {
		return err
	}
	defer d.Close()

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	// Load all gg tasks tagged "gsd" — durable work copied from GSD should use
	// this tag so the advisory audit can correlate it.
	allTasks, err := d.store.ListTasks(ctx, "")
	if err != nil {
		return fmt.Errorf("list gg tasks: %w", err)
	}
	// Build a set of title prefixes present in gg.
	ggTitlePrefixes := make(map[string]string) // prefix → gg task ID
	for _, t := range allTasks {
		for _, tag := range t.Tags {
			if tag == "gsd" {
				// Index on the [GSD:...] prefix embedded in the title.
				idx := strings.Index(t.Title, "[GSD:")
				end := strings.Index(t.Title, "]")
				if idx >= 0 && end > idx {
					prefix := t.Title[idx : end+1]
					ggTitlePrefixes[prefix] = t.ID
				}
			}
		}
	}

	var potential []string
	var recorded []string

	for _, gt := range gsdTasks {
		prefix := gt.expectedTitlePrefix()
		if ggID, ok := ggTitlePrefixes[prefix]; ok {
			recorded = append(recorded, fmt.Sprintf("%s → %s", prefix, ggID))
		} else {
			potential = append(potential, fmt.Sprintf("%s-%s-%s (%s)", gt.MilestoneID, gt.SliceID, gt.TaskID, gt.Title))
		}
	}

	fmt.Printf("GSD audit: %d tasks, %d recorded in gg, %d scratchpad-only\n\n", len(gsdTasks), len(recorded), len(potential))

	if len(recorded) > 0 {
		fmt.Println("Recorded in gg:")
		for _, m := range recorded {
			fmt.Printf("  ✓ %s\n", m)
		}
		fmt.Println()
	}

	if len(potential) > 0 {
		fmt.Println("Scratchpad-only GSD tasks:")
		for _, m := range potential {
			fmt.Printf("  • %s\n", m)
		}
		fmt.Println()
		fmt.Printf("If one of these became durable project work, record it in gg:\n")
		if len(gsdTasks) > 0 {
			gt := gsdTasks[0]
			for _, t := range gsdTasks {
				if t.Status != "complete" {
					gt = t
					break
				}
			}
			sliceNum := strings.TrimPrefix(gt.SliceID, "S")
			fmt.Printf("  gg task create \"%s short title\" \\\n", gt.expectedTitlePrefix())
			fmt.Printf("    --detail \"<scope>\" --priority medium --tags \"gsd,slice-%s\"\n", sliceNum)
		}
		return nil
	}

	fmt.Println("✓ No scratchpad-only GSD tasks found.")
	return nil
}

func readGSDTasks(ctx context.Context, dbPath string) ([]gsdAuditTask, error) {
	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro&_journal_mode=WAL")
	if err != nil {
		return nil, err
	}
	defer func() { _ = db.Close() }()

	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("open gsd.db: %w", err)
	}

	rows, err := db.QueryContext(ctx,
		`SELECT milestone_id, slice_id, id, title, status
		 FROM tasks
		 ORDER BY milestone_id, slice_id, sequence, id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []gsdAuditTask
	for rows.Next() {
		var t gsdAuditTask
		if err := rows.Scan(&t.MilestoneID, &t.SliceID, &t.TaskID, &t.Title, &t.Status); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
