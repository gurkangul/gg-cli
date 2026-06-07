package cmd

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/store"
)

var canonCmd = &cobra.Command{
	Use:   "canon",
	Short: "Distilled institutional memory — the durable knowledge every agent should start with",
	Long: `gg canon is the agent-distilled layer on top of the raw decision/bug ledger.

A new agent reads the canon and inherits the senior-dev's distilled knowledge,
instead of re-deriving it from hundreds of individual records. The canon is
injected at session-start (like RULES), stored JSONL-first (survives any rebuild),
and produced by an agent — gg has no cloud LLM (no-network), so distillation is
agent-driven:

  gg canon gather                       # dump the raw material (active decisions,
                                        #   rejections, fixed-bug root causes)
  # ...agent distills it into durable per-area knowledge...
  gg canon set architecture "JSONL is source of truth; Qdrant is a derived index…"
  gg canon show                         # what every new agent now starts with`,
}

var canonSetCmd = &cobra.Command{
	Use:   `set <area> "<distilled text>"`,
	Short: "Write/overwrite the canon for an area (e.g. architecture, auth, gotchas)",
	Args:  cobra.MinimumNArgs(2),
	RunE:  runCanonSet,
}

var canonShowCmd = &cobra.Command{
	Use:   "show [area]",
	Short: "Show the project canon (the distilled knowledge injected at session-start)",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runCanonShow,
}

var canonGatherCmd = &cobra.Command{
	Use:   "gather",
	Short: "Dump active decisions + rejections + fixed-bug root causes as raw material to distill",
	RunE:  runCanonGather,
}

func init() {
	canonCmd.AddCommand(canonSetCmd, canonShowCmd, canonGatherCmd)
	rootCmd.AddCommand(canonCmd)
}

func runCanonSet(cmd *cobra.Command, args []string) error {
	area := args[0]
	text := strings.TrimSpace(strings.Join(args[1:], " "))
	d, err := loadDepsReadOnly(false)
	if err != nil {
		return err
	}
	defer d.Close()
	if err := d.store.SetCanon(area, text, resolveAuthor(cmd)); err != nil {
		return err
	}
	fmt.Printf("✓ Canon updated: %s\n", area)
	fmt.Println("  New agents will receive this at session-start (gg canon show).")
	return nil
}

func runCanonShow(cmd *cobra.Command, args []string) error {
	d, err := loadDepsReadOnly(false)
	if err != nil {
		return err
	}
	defer d.Close()
	entries, err := d.store.ListCanon()
	if err != nil {
		return err
	}
	if len(args) == 1 {
		want := strings.ToLower(strings.TrimSpace(args[0]))
		filtered := entries[:0]
		for _, e := range entries {
			if strings.ToLower(e.Area) == want {
				filtered = append(filtered, e)
			}
		}
		entries = filtered
	}
	return printJSON(entries, func() { writeCanon(cmd.OutOrStdout(), entries) })
}

func writeCanon(w io.Writer, entries []store.CanonEntry) {
	if len(entries) == 0 {
		fmt.Fprintln(w, "No canon yet. Distill it: gg canon gather  →  gg canon set <area> \"…\"")
		return
	}
	fmt.Fprintln(w, "PROJECT CANON (distilled institutional memory):")
	for _, e := range entries {
		fmt.Fprintf(w, "\n## %s\n%s\n", e.Area, e.Text)
	}
}

func runCanonGather(cmd *cobra.Command, args []string) error {
	d, err := loadDepsReadOnly(false)
	if err != nil {
		return err
	}
	defer d.Close()
	if d.qdrantDown {
		return fmt.Errorf("qdrant unreachable — gather needs the ledger; retry when stores are up")
	}
	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()
	w := cmd.OutOrStdout()
	fmt.Fprintln(w, "=== RAW MATERIAL TO DISTILL INTO CANON ===")
	fmt.Fprintln(w, "Read this, then write durable per-area knowledge with: gg canon set <area> \"…\"")

	if decs, err := d.store.ListDecisions(ctx, 0, false); err == nil && len(decs) > 0 {
		fmt.Fprintf(w, "\n--- ACTIVE DECISIONS (%d) ---\n", len(decs))
		for _, dd := range decs {
			fmt.Fprintf(w, "• %s", dd.Text)
			if dd.Reason != "" {
				fmt.Fprintf(w, " — %s", dd.Reason)
			}
			fmt.Fprintln(w)
		}
	}
	if rej, err := d.store.ListRejections(ctx, 0); err == nil && len(rej) > 0 {
		fmt.Fprintf(w, "\n--- REJECTED APPROACHES (%d) — do not re-propose ---\n", len(rej))
		for _, r := range rej {
			fmt.Fprintf(w, "✗ %s", r.Approach)
			if r.Reason != "" {
				fmt.Fprintf(w, " — %s", r.Reason)
			}
			fmt.Fprintln(w)
		}
	}
	if bugs, err := d.store.ListBugs(ctx, "fixed"); err == nil && len(bugs) > 0 {
		fmt.Fprintf(w, "\n--- FIXED BUGS / ROOT CAUSES (%d) — known failure modes ---\n", len(bugs))
		for _, b := range bugs {
			fmt.Fprintf(w, "✓ %s: %s", b.ID, b.Title)
			if b.RootCause != "" {
				fmt.Fprintf(w, " [root cause: %s]", b.RootCause)
			}
			fmt.Fprintln(w)
		}
	}
	return nil
}
