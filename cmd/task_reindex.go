package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/graph"
)

var taskReindexCmd = &cobra.Command{
	Use:   "reindex",
	Short: "Replay Task nodes into the code graph from the task store",
	Long: `Rebuild Task nodes in the code graph from the task store.

Use this to heal drift that occurs when (a) the code graph was unavailable
during gg task create, or (b) tasks were created before gg started dual-writing
(TASK-225). The task store holds the authoritative task list; reindex upserts a
matching Task node for each one, idempotently.

Only node identity (id + title) is mirrored. Status, priority,
tags, and author remain in the task store — code-graph Task nodes exist to
participate in graph traversal (DECIDES / IMPLEMENTS / IN_WAVE edges).`,
	RunE: runTaskReindex,
}

func init() {
	taskCmd.AddCommand(taskReindexCmd)
}

func runTaskReindex(cmd *cobra.Command, _ []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
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

	gc, err := graph.New(cfg.DataDir, cfg.ProjectID)
	if err != nil {
		return fmt.Errorf("memgraph: %w", err)
	}
	defer func() { _ = gc.Close(ctx) }()

	var replayed, failed, edgesWritten int
	for _, t := range tasks {
		gctx, gcancel := withTimeout(cmd.Context())
		upErr := gc.UpsertTaskNode(gctx, t.ID, t.Title)
		gcancel()
		if upErr != nil {
			fmt.Printf("~ %s: %v\n", t.ID, upErr)
			failed++
			continue
		}
		replayed++
	}

	// Second pass: write DEPENDS_ON + BLOCKS edges after every Task node
	// exists so MATCH endpoints always resolve.
	for _, t := range tasks {
		for _, dep := range t.DependsOn {
			if dep == "" {
				continue
			}
			ectx, ecancel := withTimeout(cmd.Context())
			if eErr := gc.UpsertDependsOnEdge(ectx, t.ID, dep); eErr != nil {
				fmt.Printf("~ %s DEPENDS_ON %s: %v\n", t.ID, dep, eErr)
			} else {
				edgesWritten++
			}
			ecancel()
		}
		for _, bl := range t.Blocks {
			if bl == "" {
				continue
			}
			ectx, ecancel := withTimeout(cmd.Context())
			if eErr := gc.UpsertBlocksEdge(ectx, t.ID, bl); eErr != nil {
				fmt.Printf("~ %s BLOCKS %s: %v\n", t.ID, bl, eErr)
			} else {
				edgesWritten++
			}
			ecancel()
		}
	}

	return printJSON(map[string]any{
		"replayed":      replayed,
		"failed":        failed,
		"edges_written": edgesWritten,
	}, func() {
		fmt.Printf("task reindex complete: %d replayed, %d failed, %d edges\n", replayed, failed, edgesWritten)
	})
}
