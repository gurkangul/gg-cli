package cmd

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/brain"
	"github.com/gurkangul/gg-cli/internal/config"
)

// backlinks.go — TASK-517 reverse link traversal over the brain ledger.
//
// gg could always answer "what does this decision point at" (task_id,
// depends_on, blocks) but never the reverse: "what points AT this". That
// reverse view is the primitive a linked knowledge base is built on — it turns
// a pile of records into a web an agent can walk backwards from any anchor.
//
// It is computed live from the folded JSONL (see internal/brain/links.go), so
// it needs neither the embedder nor Memgraph and can never drift from the
// ledger. Prose refs count too: writing [[TASK-042]] or just naming TASK-042 in
// a decision's reason creates a real, reversible edge with no extra flag.

var (
	backlinksCompact  bool
	backlinksOutgoing bool
)

var backlinksCmd = &cobra.Command{
	Use:   "backlinks <ref>",
	Short: "Show every brain entry that references this task, bug, or decision",
	Long: `List the entries that link TO a reference — the reverse of the links gg
already stores, plus [[wiki links]] and bare TASK-NNN / BUG-NNN mentions found in
free text.

WHEN TO USE: before changing or closing something, to see what else depends on
it — "what decisions reference TASK-042?", "what tasks were spawned by BUG-084?".

<ref> may be a TASK-NNN, a BUG-NNN, a record uuid, or an exact title.

Reads the JSONL ledger directly, so it works with the vector store and the code
graph both offline.

See also: gg impact (code + task blast radius), gg search (find by meaning)`,
	Args: cobra.ExactArgs(1),
	RunE: runBacklinks,
}

func init() {
	backlinksCmd.Flags().BoolVar(&backlinksCompact, "compact", false, "one line per link — preserves agent context window")
	backlinksCmd.Flags().BoolVar(&backlinksOutgoing, "outgoing", false, "also list what this entry links OUT to")
	rootCmd.AddCommand(backlinksCmd)
}

func runBacklinks(cmd *cobra.Command, args []string) error {
	ref, err := requireNonEmpty("ref", args[0])
	if err != nil {
		return err
	}

	ggDir := config.GGDirOrEmpty()
	if ggDir == "" {
		return configErr("not inside a gg project — run gg init first")
	}

	hits, err := brain.Backlinks(ggDir, ref)
	if err != nil {
		return fmt.Errorf("read backlinks: %w", err)
	}

	anchor, anchorKind, resolved := brain.FindByRef(ggDir, ref)

	var outgoing []brain.Ref
	if backlinksOutgoing && resolved {
		outgoing = brain.OutgoingRefs(anchor)
	}

	jsonMap := map[string]any{
		"ref":         ref,
		"resolved":    resolved,
		"anchor_kind": anchorKind,
		"backlinks":   backlinkJSON(hits),
		"outgoing":    outgoing,
		"count":       len(hits),
	}

	return printJSON(jsonMap, func() {
		if isCompactActive(cmd) {
			emitCompact(cmd, "backlinks",
				func(w io.Writer) { renderBacklinksDefault(w, ref, anchor, anchorKind, resolved, hits, outgoing) },
				func(w io.Writer) { renderBacklinksCompact(w, ref, hits, outgoing) },
				compactRendererV_backlinks,
			)
			return
		}
		renderBacklinksDefault(cmd.OutOrStdout(), ref, anchor, anchorKind, resolved, hits, outgoing)
	})
}

func backlinkJSON(hits []brain.LinkHit) []map[string]any {
	out := make([]map[string]any, 0, len(hits))
	for _, h := range hits {
		out = append(out, map[string]any{
			"kind":       h.Kind,
			"uuid":       h.Entry.UUID,
			"id":         brain.SelfIDForKind(h.Entry, h.Kind),
			"summary":    brainEntrySummary(h.Entry),
			"created_at": h.Entry.CreatedAt,
			"via":        h.Via,
		})
	}
	return out
}

// brainEntrySummary picks the human-facing line for an entry, whichever payload
// field its kind uses to carry the headline.
func brainEntrySummary(e brain.Entry) string {
	for _, key := range []string{"title", "text", "approach", "content"} {
		if v, ok := e.Payload[key].(string); ok {
			if s := strings.TrimSpace(v); s != "" {
				return s
			}
		}
	}
	return "(no summary)"
}

// brainEntryDate returns the entry's creation date. Legacy JSONL lines carry it
// only inside the payload (the top-level created_at arrived with TASK-362), so
// fall back there rather than rendering an empty column.
func brainEntryDate(e brain.Entry) string {
	if s := strings.TrimSpace(e.CreatedAt); s != "" {
		return shortDate(s)
	}
	if v, ok := e.Payload["created_at"].(string); ok {
		if s := strings.TrimSpace(v); s != "" {
			return shortDate(s)
		}
	}
	return "—"
}

// backlinkLabel is the identity column: the TASK-/BUG- id when the entry has
// one, otherwise its date (a uuid is noise in a terminal list).
func backlinkLabel(e brain.Entry, kind string) string {
	if id := brain.SelfIDForKind(e, kind); id != "" && id != e.UUID {
		return id
	}
	return brainEntryDate(e)
}

// backlinkKindIcon maps a brain kind to the one-letter marker used elsewhere in
// gg output (D decision, R rejection, T task, B bug, N note, M message).
func backlinkKindIcon(kind string) string {
	switch kind {
	case "decisions":
		return "D"
	case "rejections":
		return "R"
	case "tasks":
		return "T"
	case "bugs":
		return "B"
	case "notes":
		return "N"
	case "messages":
		return "M"
	default:
		return "?"
	}
}

func renderBacklinksDefault(w io.Writer, ref string, anchor brain.Entry, anchorKind string, resolved bool, hits []brain.LinkHit, outgoing []brain.Ref) {
	if resolved {
		fmt.Fprintf(w, "BACKLINKS → %s (%s: %s)\n\n", strings.ToUpper(ref), strings.TrimSuffix(anchorKind, "s"), compactTrim(brainEntrySummary(anchor), 80))
	} else {
		fmt.Fprintf(w, "BACKLINKS → %s (not found in the ledger — showing references anyway)\n\n", strings.ToUpper(ref))
	}

	if len(hits) == 0 {
		fmt.Fprintln(w, "  (nothing links here)")
	}
	for _, h := range hits {
		id := backlinkLabel(h.Entry, h.Kind)
		fmt.Fprintf(w, "  %s %-12s %s\n", backlinkKindIcon(h.Kind), id, compactTrim(brainEntrySummary(h.Entry), 90))
		fmt.Fprintf(w, "      via: %s\n", strings.Join(h.Via, ", "))
	}

	if len(outgoing) > 0 {
		fmt.Fprintln(w, "\nOUTGOING:")
		for _, r := range outgoing {
			fmt.Fprintf(w, "  → %-20s (%s)\n", r.Raw, r.Via)
		}
	}
}

func renderBacklinksCompact(w io.Writer, ref string, hits []brain.LinkHit, outgoing []brain.Ref) {
	fmt.Fprintf(w, "backlinks %s — %d in", strings.ToUpper(ref), len(hits))
	if len(outgoing) > 0 {
		fmt.Fprintf(w, " %d out", len(outgoing))
	}
	fmt.Fprintf(w, "\n\n")

	if len(hits) == 0 {
		fmt.Fprintln(w, "(nothing links here)")
	}
	for _, h := range hits {
		id := backlinkLabel(h.Entry, h.Kind)
		fmt.Fprintf(w, "← %s %s %s [%s]\n", backlinkKindIcon(h.Kind), id, compactTrim(brainEntrySummary(h.Entry), 70), strings.Join(h.Via, ","))
	}
	for _, r := range outgoing {
		fmt.Fprintf(w, "→ %s [%s]\n", compactTrim(r.Raw, 60), r.Via)
	}
}
