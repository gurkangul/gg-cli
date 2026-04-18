package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/graph"
)

var bugReindexCmd = &cobra.Command{
	Use:   "reindex",
	Short: "Replay bug AFFECTS edges into Memgraph",
	Long: `Rebuild Bug→File and Bug→Symbol edges in Memgraph from the Qdrant store.

Use this to heal drift that occurs when Memgraph was unreachable during
gg bug report — Qdrant holds the authoritative affected_files /
affected_symbols lists; reindex replays them into the graph.

By default all bugs with at least one affected file or symbol are
replayed. Use --since to limit to bugs updated on or after a date
(RFC 3339, e.g. 2026-04-01T00:00:00Z).`,
	RunE: runBugReindex,
}

var bugReindexSince string

func init() {
	bugReindexCmd.Flags().StringVar(&bugReindexSince, "since", "", "only reindex bugs updated on or after this RFC 3339 timestamp")
	bugCmd.AddCommand(bugReindexCmd)
}

func runBugReindex(cmd *cobra.Command, _ []string) error {
	var sinceTime time.Time
	if bugReindexSince != "" {
		t, err := time.Parse(time.RFC3339, bugReindexSince)
		if err != nil {
			return fmt.Errorf("--since: %w (expected RFC 3339, e.g. 2026-04-01T00:00:00Z)", err)
		}
		sinceTime = t
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if cfg.Memgraph.URI == "" {
		return fmt.Errorf("memgraph.uri not configured — run gg init first")
	}

	d, err := loadDeps(false)
	if err != nil {
		return err
	}
	defer d.Close()

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	bugs, err := d.store.ListBugs(ctx, "")
	if err != nil {
		return fmt.Errorf("list bugs: %w", err)
	}

	gc, err := graph.New(&cfg.Memgraph, cfg.ProjectID)
	if err != nil {
		return fmt.Errorf("memgraph: %w", err)
	}
	defer func() { _ = gc.Close(ctx) }()

	var skipped, replayed, failed int
	for _, b := range bugs {
		if len(b.AffectedFiles) == 0 && len(b.AffectedSymbols) == 0 {
			skipped++
			continue
		}
		if !sinceTime.IsZero() {
			ts, parseErr := time.Parse(time.RFC3339, b.UpdatedAt)
			if parseErr != nil || ts.Before(sinceTime) {
				skipped++
				continue
			}
		}
		gctx, gcancel := withTimeout(cmd.Context())
		mergeErr := gc.ReplaceBugAffects(gctx, b.ID, b.Title, b.AffectedFiles, b.AffectedSymbols)
		gcancel()
		if mergeErr != nil {
			fmt.Printf("~ %s: %v\n", b.ID, mergeErr)
			failed++
		} else {
			replayed++
		}
	}

	return printJSON(map[string]any{
		"replayed": replayed,
		"failed":   failed,
		"skipped":  skipped,
	}, func() {
		fmt.Printf("reindex complete: %d replayed, %d failed, %d skipped\n", replayed, failed, skipped)
	})
}
