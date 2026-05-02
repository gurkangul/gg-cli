package cmd

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	_ "modernc.org/sqlite" // registers "sqlite" driver

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/orchestrator/terminal"
)

var gsdCmd = &cobra.Command{
	Use:   "gsd",
	Short: "GSD integration utilities",
}

var gsdOpenCmd = &cobra.Command{
	Use:   "open",
	Short: "Open interactive GSD in a new terminal pane",
	Long: `Open interactive GSD in a new terminal pane rooted at the current gg project.

This is the stable pane-launch path for GSD. It starts the interactive TUI;
headless commands such as 'gsd headless query' only report state and do not
open a tab or pane.`,
	RunE: runGSDOpen,
}

var gsdAuditCmd = &cobra.Command{
	Use:   "audit",
	Short: "Audit GSD ↔ gg task mirrors — report missing per-T-task gg mirrors",
	Long: `Scans .gsd/gsd.db for T-level tasks and compares them against gg tasks
tagged "gsd". Each GSD task should have exactly one gg task mirror whose
title contains [GSD:<milestone_id>-<slice_id>-<task_id>].

Exit codes:
  0  all GSD tasks are mirrored in gg
  1  drift found (missing mirrors)
`,
	RunE: runGSDaudit,
}

var gsdAuditProject string
var gsdOpenAgent string
var gsdOpenSplit string

func init() {
	gsdOpenCmd.Flags().StringVar(&gsdOpenAgent, "agent", "", "command to run in the pane (default: $GG_SPAWN_AGENT or 'gsd')")
	gsdOpenCmd.Flags().StringVar(&gsdOpenSplit, "split", "vertical", "pane split direction: horizontal (below) or vertical (right, default)")
	gsdCmd.AddCommand(gsdOpenCmd)

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

func runGSDOpen(cmd *cobra.Command, _ []string) error {
	projectRoot, err := config.FindRoot()
	if err != nil {
		return err
	}

	agentCmd := gsdOpenAgent
	if agentCmd == "" {
		agentCmd = spawnAgentDefault()
	}
	if err := validateGSDOpenAgent(agentCmd); err != nil {
		return err
	}

	splitDir := terminal.SplitHorizontal
	if strings.ToLower(gsdOpenSplit) == "vertical" {
		splitDir = terminal.SplitVertical
	}

	term, err := terminal.NewFromEnv()
	if err != nil {
		return fmt.Errorf("terminal backend: %w", err)
	}

	env := buildWorkerEnv("", nil)
	surfaceID, err := term.NewSplit(cmd.Context(), terminal.SplitOpts{
		Dir: splitDir,
		Env: env,
	})
	if err != nil {
		return fmt.Errorf("open GSD pane: %w", err)
	}

	launch := buildGSDOpenLaunchCommand(projectRoot, agentCmd)
	if err := term.Send(cmd.Context(), surfaceID, launch); err != nil {
		return fmt.Errorf("launch GSD in pane %s: %w", surfaceID, err)
	}
	if err := term.SendKey(cmd.Context(), surfaceID, "enter"); err != nil {
		return fmt.Errorf("send Enter after GSD launch: %w", err)
	}

	return printJSON(map[string]any{
		"surface_id": string(surfaceID),
		"agent":      agentCmd,
		"project":    projectRoot,
	}, func() {
		fmt.Printf("✓ GSD pane %s opened (agent: %s, project: %s)\n", surfaceID, agentCmd, projectRoot)
	})
}

func validateGSDOpenAgent(agentCmd string) error {
	fields := strings.Fields(agentCmd)
	if len(fields) == 0 {
		return fmt.Errorf("empty GSD agent command")
	}
	if _, err := exec.LookPath(fields[0]); err != nil {
		return fmt.Errorf("GSD command %q not found on PATH — install GSD or pass --agent", fields[0])
	}
	return nil
}

func buildGSDOpenLaunchCommand(projectRoot, agentCmd string) string {
	parts := []string{"cd " + shellQuote(projectRoot)}
	if v := os.Getenv("GG_AGENT"); v != "" {
		parts = append(parts, "export GG_AGENT="+shellQuote(v))
	}
	if v := os.Getenv("GG_ROLE"); v != "" {
		parts = append(parts, "export GG_ROLE="+shellQuote(v))
	}
	return strings.Join(parts, " && ") + " && exec " + agentCmd
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
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

	// Load all gg tasks tagged "gsd" — the mirror convention.
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

	var missing []string
	var mirrored []string

	for _, gt := range gsdTasks {
		prefix := gt.expectedTitlePrefix()
		if ggID, ok := ggTitlePrefixes[prefix]; ok {
			mirrored = append(mirrored, fmt.Sprintf("%s → %s", prefix, ggID))
		} else {
			missing = append(missing, fmt.Sprintf("%s-%s-%s (%s)", gt.MilestoneID, gt.SliceID, gt.TaskID, gt.Title))
		}
	}

	fmt.Printf("GSD audit: %d tasks, %d mirrored, %d missing\n\n", len(gsdTasks), len(mirrored), len(missing))

	if len(mirrored) > 0 {
		fmt.Println("Mirrored:")
		for _, m := range mirrored {
			fmt.Printf("  ✓ %s\n", m)
		}
		fmt.Println()
	}

	if len(missing) > 0 {
		fmt.Println("Missing mirrors:")
		for _, m := range missing {
			fmt.Printf("  ✗ %s\n", m)
		}
		fmt.Println()
		fmt.Printf("To create a missing mirror:\n")
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
		return &ExitError{Code: ExitGeneral, Message: fmt.Sprintf("drift: %d GSD task(s) have no gg mirror", len(missing))}
	}

	fmt.Println("✓ All GSD tasks are mirrored in gg.")
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
