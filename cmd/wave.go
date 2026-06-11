package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/graph"
)

var waveCmd = &cobra.Command{
	Use:   "wave",
	Short: "Manage optional wave/milestone buckets — sprints (Memgraph only)",
	Long: `Wave nodes group tasks into time-bounded sprints or milestones.
Waves are OPTIONAL: projects that don't do time-based planning can ignore this
command entirely — nothing else depends on waves existing.
They live in Memgraph only — no Qdrant collection is created.

  gg wave add wave1 --name "Phase 1" --goal "Ship MVP" --start 2026-01-01 --end 2026-03-31
  gg wave list
  gg wave list --active
  gg wave status wave1
  gg wave assign TASK-042 --wave wave1`,
}

var (
	waveAddName      string
	waveAddGoal      string
	waveAddStart     string
	waveAddEnd       string
	waveAddStatus    string
	waveListActive   bool
	waveAssignWaveID string
	waveMigrateApply bool
)

func init() {
	addCmd := &cobra.Command{
		Use:   "add <wave-id>",
		Short: "Create or update a wave node",
		Args:  cobra.ExactArgs(1),
		RunE:  runWaveAdd,
	}
	addCmd.Flags().StringVar(&waveAddName, "name", "", "wave display name")
	addCmd.Flags().StringVar(&waveAddGoal, "goal", "", "sprint/milestone goal")
	addCmd.Flags().StringVar(&waveAddStart, "start", "", "start date (YYYY-MM-DD)")
	addCmd.Flags().StringVar(&waveAddEnd, "end", "", "end date (YYYY-MM-DD)")
	addCmd.Flags().StringVar(&waveAddStatus, "status", "active", "wave status: active or closed")

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List waves",
		RunE:  runWaveList,
	}
	listCmd.Flags().BoolVar(&waveListActive, "active", false, "show active waves only")

	statusCmd2 := &cobra.Command{
		Use:   "status <wave-id>",
		Short: "Show wave details and assigned tasks",
		Args:  cobra.ExactArgs(1),
		RunE:  runWaveStatus,
	}

	assignCmd := &cobra.Command{
		Use:   "assign <task-id>",
		Short: "Assign a task to a wave",
		Args:  cobra.ExactArgs(1),
		RunE:  runWaveAssign,
	}
	assignCmd.Flags().StringVar(&waveAssignWaveID, "wave", "", "wave ID to assign to (required)")
	_ = assignCmd.MarkFlagRequired("wave")

	migrateCmd := &cobra.Command{
		Use:   "migrate-tags",
		Short: "Dry-run tag-to-wave migration (--apply to execute)",
		Long: `Scan tasks with wave* tags (e.g. wave1, wave2) and report which would be
assigned to Wave nodes. By default this is a dry-run — no Memgraph writes.
Pass --apply to create Wave nodes and write IN_WAVE edges.`,
		RunE: runWaveMigrateTags,
	}
	migrateCmd.Flags().BoolVar(&waveMigrateApply, "apply", false, "write Wave nodes and IN_WAVE edges (default: dry-run report only)")

	waveCmd.AddCommand(addCmd, listCmd, statusCmd2, assignCmd, migrateCmd)
	rootCmd.AddCommand(waveCmd)
}

func runWaveAdd(cmd *cobra.Command, args []string) error {
	waveID, err := requireNonEmpty("wave-id", args[0])
	if err != nil {
		return err
	}

	g, err := loadGraph()
	if err != nil {
		return err
	}
	defer func() { _ = g.Close(cmd.Context()) }()

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	if err := g.UpsertWave(ctx, waveID,
		strings.TrimSpace(waveAddName),
		strings.TrimSpace(waveAddGoal),
		strings.TrimSpace(waveAddStart),
		strings.TrimSpace(waveAddEnd),
		strings.TrimSpace(waveAddStatus),
	); err != nil {
		return fmt.Errorf("upsert wave: %w", err)
	}

	return printJSON(map[string]any{"id": waveID, "status": waveAddStatus}, func() {
		fmt.Printf("✓ Wave %q created/updated\n", waveID)
	})
}

func runWaveList(cmd *cobra.Command, _ []string) error {
	g, err := loadGraph()
	if err != nil {
		return err
	}
	defer func() { _ = g.Close(cmd.Context()) }()

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	statusFilter := ""
	if waveListActive {
		statusFilter = "active"
	}

	waves, err := g.ListWaves(ctx, statusFilter)
	if err != nil {
		return fmt.Errorf("list waves: %w", err)
	}

	return printJSON(waves, func() {
		if len(waves) == 0 {
			fmt.Println("No waves found.")
			return
		}
		for _, w := range waves {
			icon := "○"
			if w.Status == "active" {
				icon = "→"
			}
			dates := ""
			if w.StartDate != "" || w.EndDate != "" {
				dates = fmt.Sprintf(" [%s → %s]", w.StartDate, w.EndDate)
			}
			fmt.Printf("%s %-12s %s%s\n", icon, w.ID, w.Name, dates)
			if w.Goal != "" {
				fmt.Printf("  Goal: %s\n", w.Goal)
			}
		}
	})
}

func runWaveStatus(cmd *cobra.Command, args []string) error {
	waveID := args[0]

	g, err := loadGraph()
	if err != nil {
		return err
	}
	defer func() { _ = g.Close(cmd.Context()) }()

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	wave, tasks, err := g.WaveStatus(ctx, waveID)
	if err != nil {
		return fmt.Errorf("wave status: %w", err)
	}

	return printJSON(map[string]any{"wave": wave, "tasks": tasks}, func() {
		fmt.Printf("Wave: %s (%s)\n", wave.Name, wave.ID)
		if wave.Goal != "" {
			fmt.Printf("Goal: %s\n", wave.Goal)
		}
		fmt.Printf("Period: %s → %s\n", wave.StartDate, wave.EndDate)
		fmt.Printf("Status: %s\n", wave.Status)
		fmt.Printf("Tasks (%d): %s\n", len(tasks), strings.Join(tasks, ", "))
	})
}

func runWaveAssign(cmd *cobra.Command, args []string) error {
	taskID, err := requireTaskID(args[0])
	if err != nil {
		return err
	}

	g, err := loadGraph()
	if err != nil {
		return err
	}
	defer func() { _ = g.Close(cmd.Context()) }()

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	if err := g.AssignTaskToWave(ctx, taskID, waveAssignWaveID); err != nil {
		return fmt.Errorf("assign: %w", err)
	}

	return printJSON(map[string]any{"task": taskID, "wave": waveAssignWaveID}, func() {
		fmt.Printf("✓ %s assigned to wave %q\n", taskID, waveAssignWaveID)
	})
}

func runWaveMigrateTags(cmd *cobra.Command, _ []string) error {
	d, err := loadDeps(false)
	if err != nil {
		return err
	}
	defer d.Close()

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	tasks, err := d.store.ListTasks(ctx, "")
	if err != nil {
		return fmt.Errorf("list tasks: %w", err)
	}

	// Group tasks by wave tag (tags matching "wave*" pattern).
	type match struct {
		taskID string
		waveID string
	}
	var matches []match
	for _, t := range tasks {
		for _, tag := range t.Tags {
			if strings.HasPrefix(tag, "wave") {
				matches = append(matches, match{taskID: t.ID, waveID: tag})
			}
		}
	}

	if len(matches) == 0 {
		fmt.Println("No tasks with wave* tags found.")
		return nil
	}

	fmt.Printf("Found %d task-wave assignments:\n", len(matches))
	for _, m := range matches {
		fmt.Printf("  %s → %s\n", m.taskID, m.waveID)
	}

	if !waveMigrateApply {
		fmt.Printf("\nDry-run complete. Pass --apply to write %d IN_WAVE edge(s).\n", len(matches))
		return nil
	}

	g, err := loadGraph()
	if err != nil {
		return err
	}
	defer func() { _ = g.Close(cmd.Context()) }()

	written := 0
	for _, m := range matches {
		// Ensure the Wave node exists (upsert with minimal fields).
		if upsertErr := g.UpsertWave(ctx, m.waveID, m.waveID, "", "", "", "active"); upsertErr != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "⚠ upsert wave %q: %v\n", m.waveID, upsertErr)
			continue
		}
		if assignErr := g.AssignTaskToWave(ctx, m.taskID, m.waveID); assignErr != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "⚠ assign %s → %s: %v\n", m.taskID, m.waveID, assignErr)
			continue
		}
		written++
	}
	fmt.Printf("✓ Wrote %d/%d IN_WAVE edges.\n", written, len(matches))
	return nil
}

func loadGraph() (*graph.Client, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	return graph.New(&cfg.Memgraph, cfg.ProjectID)
}
