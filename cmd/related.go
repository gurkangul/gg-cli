package cmd

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/brain"
	"github.com/gurkangul/gg-cli/internal/config"
)

// related.go — TASK-518 multi-hop walk over the derived link graph.
//
// gg context and gg impact already surface "related decisions", but they do it
// with vector similarity — things that SOUND alike. That is a different question
// from things that are actually CONNECTED. A decision can be the direct cause of
// a bug and share almost no vocabulary with it.
//
// gg related walks the real edges (see internal/brain/linkgraph.go): structured
// relations plus prose refs, resolved against the ledger. It needs neither the
// embedder nor Memgraph, so it keeps working when semantic recall is degraded —
// which is exactly when an agent most needs a reliable way to orient.

var (
	relatedHops    int
	relatedCompact bool
)

var relatedCmd = &cobra.Command{
	Use:   "related <ref>",
	Short: "Walk the link graph outward from a task, bug, or decision",
	Long: `Show what is CONNECTED to a reference, not merely what sounds similar.

Traverses the real link graph — task_id / depends_on / blocks plus [[wiki links]]
and TASK-NNN / BUG-NNN mentions in prose — outward from <ref>, nearest first.
Traversal is undirected: both what points at the anchor and what it points to are
part of the neighbourhood, with the direction shown per edge.

WHEN TO USE: orienting before a change — "what is entangled with BUG-084?" — or
when semantic search is degraded, since this reads the JSONL ledger directly and
needs neither the vector store nor the code graph.

See also: gg backlinks (one hop, reverse), gg impact (code blast radius)`,
	Args: cobra.ExactArgs(1),
	RunE: runRelated,
}

func init() {
	relatedCmd.Flags().IntVar(&relatedHops, "hops", 2, "how many hops to walk outward")
	relatedCmd.Flags().BoolVar(&relatedCompact, "compact", false, "one line per node — preserves agent context window")
	rootCmd.AddCommand(relatedCmd)
}

func runRelated(cmd *cobra.Command, args []string) error {
	ref, err := requireNonEmpty("ref", args[0])
	if err != nil {
		return err
	}
	if relatedHops < 1 {
		return fmt.Errorf("--hops must be at least 1")
	}

	ggDir := config.GGDirOrEmpty()
	if ggDir == "" {
		return configErr("not inside a gg project — run gg init first")
	}

	g, err := brain.LoadLinkGraph(ggDir)
	if err != nil {
		return fmt.Errorf("load link graph: %w", err)
	}

	anchor, resolved := g.Lookup(ref)
	if !resolved {
		return fmt.Errorf("%q is not in the ledger — pass a TASK-NNN, BUG-NNN, uuid, or exact title", ref)
	}

	hits := g.Related(ref, relatedHops)
	dangling := g.Dangling(ref)

	jsonMap := map[string]any{
		"ref":      anchor.ID,
		"kind":     anchor.Kind,
		"hops":     relatedHops,
		"count":    len(hits),
		"related":  relatedJSON(hits),
		"dangling": dangling,
	}

	return printJSON(jsonMap, func() {
		if isCompactActive(cmd) {
			emitCompact(cmd, "related",
				func(w io.Writer) { renderRelatedDefault(w, anchor, hits, dangling) },
				func(w io.Writer) { renderRelatedCompact(w, anchor, hits) },
				compactRendererV_related,
			)
			return
		}
		renderRelatedDefault(cmd.OutOrStdout(), anchor, hits, dangling)
	})
}

func relatedJSON(hits []brain.Related) []map[string]any {
	out := make([]map[string]any, 0, len(hits))
	for _, h := range hits {
		out = append(out, map[string]any{
			"id":        h.Node.ID,
			"kind":      h.Node.Kind,
			"uuid":      h.Node.Entry.UUID,
			"summary":   brainEntrySummary(h.Node.Entry),
			"hops":      h.Hops,
			"via":       h.Via,
			"direction": h.Direction,
			"from":      h.From,
		})
	}
	return out
}

// arrowFor renders edge direction relative to the anchor: ← reached because it
// points at us, → reached because we point at it.
func arrowFor(direction string) string {
	if direction == "in" {
		return "←"
	}
	return "→"
}

func renderRelatedDefault(w io.Writer, anchor brain.LinkNode, hits []brain.Related, dangling []brain.Ref) {
	fmt.Fprintf(w, "RELATED → %s (%s: %s)\n",
		anchor.ID, strings.TrimSuffix(anchor.Kind, "s"), compactTrim(brainEntrySummary(anchor.Entry), 80))

	if len(hits) == 0 {
		fmt.Fprintln(w, "\n  (nothing connected within range)")
	}

	lastHop := 0
	for _, h := range hits {
		if h.Hops != lastHop {
			fmt.Fprintf(w, "\n  %d hop(s) away:\n", h.Hops)
			lastHop = h.Hops
		}
		label := h.Node.ID
		if label == h.Node.Entry.UUID {
			label = brainEntryDate(h.Node.Entry)
		}
		fmt.Fprintf(w, "    %s %s %-12s %s\n",
			arrowFor(h.Direction), backlinkKindIcon(h.Node.Kind), label,
			compactTrim(brainEntrySummary(h.Node.Entry), 80))
		fmt.Fprintf(w, "        via: %s\n", h.Via)
	}

	if len(dangling) > 0 {
		fmt.Fprintln(w, "\n  UNRESOLVED references written on this entry:")
		for _, r := range dangling {
			fmt.Fprintf(w, "    ? %s (%s)\n", compactTrim(r.Raw, 60), r.Via)
		}
	}
}

func renderRelatedCompact(w io.Writer, anchor brain.LinkNode, hits []brain.Related) {
	fmt.Fprintf(w, "related %s — %d node(s)\n\n", anchor.ID, len(hits))
	if len(hits) == 0 {
		fmt.Fprintln(w, "(nothing connected within range)")
		return
	}
	for _, h := range hits {
		label := h.Node.ID
		if label == h.Node.Entry.UUID {
			label = brainEntryDate(h.Node.Entry)
		}
		fmt.Fprintf(w, "h%d %s %s %s %s [%s]\n",
			h.Hops, arrowFor(h.Direction), backlinkKindIcon(h.Node.Kind), label,
			compactTrim(brainEntrySummary(h.Node.Entry), 60), h.Via)
	}
}
