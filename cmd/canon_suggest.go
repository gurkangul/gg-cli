package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/store"
)

// gg canon suggest is a NO-LLM, no-network consolidation *ritual*. gg has no
// cloud LLM (no-network) — the agent does the thinking. `suggest` emits a
// deterministic packet so the agent can see distilled-vs-raw and fill a
// structured op contract:
//
//  1. DISTILLED — the current canon the agent already inherits: curated areas
//     (editable, JSONL-backed) + auto-derived areas (live digest of the ledger).
//  2. RAW DELTA — a bounded set of recent raw ledger entries (decisions,
//     rejections, fixed-bug root causes) whose text is NOT yet reflected in a
//     curated canon area, so the agent sees what the distilled layer is missing.
//  3. OP CONTRACT — a structured add/edit/delete template the agent fills, each
//     op typed (decision/constraint/invariant/failure-mode/rejection) + tagged +
//     carrying a dedup hint vs existing curated areas. Reference shape:
//     wrongstack memory-consolidator.ts buildConsolidationPrompt (the LLM call
//     itself is intentionally NOT ported — gg stays offline).
//
// APPLY PATH (documented choice): the curated layer IS directly editable
// (store.SetCanon, last-write-wins per area), so an apply path fits and is
// added as `gg canon apply --ops <file|->`. The auto-derived layer is computed
// live from the ledger and is NOT editable — to change it the agent edits the
// underlying records (gg record / gg reject / gg bug). `suggest` therefore
// targets curated areas only and prints ready-to-run `gg canon set` guidance
// alongside the machine-applyable contract.

// canonSuggestOp is one structured consolidation operation the agent fills in.
// It mirrors wrongstack ConsolidationOp (action/text/query/type/tags) adapted to
// gg's per-area curated canon: `Area` is the durable bucket, `DedupHint` is the
// "query" — which existing curated area/raw entry this op overlaps with.
type canonSuggestOp struct {
	Action    string   `json:"action"`               // add | edit | delete
	Area      string   `json:"area"`                 // curated canon bucket (e.g. architecture, auth, gotchas)
	Type      string   `json:"type,omitempty"`       // decision | constraint | invariant | failure-mode | rejection
	Text      string   `json:"text,omitempty"`       // distilled durable knowledge (add/edit)
	Tags      []string `json:"tags,omitempty"`       // grouping tags, e.g. ["sqlite","offline"]
	DedupHint string   `json:"dedup_hint,omitempty"` // existing area/raw text this overlaps; "" if net-new
}

// canonSuggestContract is the JSON object the agent returns / hands to
// `gg canon apply --ops`.
type canonSuggestContract struct {
	Operations []canonSuggestOp `json:"operations"`
}

// canonRawDeltaCap bounds how many of each raw kind appear in the suggest packet
// so the agent's context window stays lean (raw is a sample, not a dump).
const canonRawDeltaCap = 12

var canonSuggestCmd = &cobra.Command{
	Use:   "suggest",
	Short: "Emit the distilled-vs-raw packet + a structured op contract the agent fills (NO LLM, offline)",
	Long: `gg canon suggest is a no-LLM, no-network consolidation ritual.

gg has no cloud LLM — the AGENT does the distilling. suggest emits a deterministic
packet so the agent can converge the canon by hand:

  DISTILLED   the canon already inherited (curated + auto-derived)
  RAW DELTA   recent ledger entries not yet reflected in a curated area
  OP CONTRACT a structured add/edit/delete template (typed + tagged + dedup hint)

Fill the contract, then apply it:
  gg canon suggest --json > ops.json     # edit the "operations" array
  gg canon apply --ops ops.json          # deterministic, offline
  # …or run the printed 'gg canon set <area> "…"' lines directly.

The auto-derived layer is a live digest of the ledger and is NOT editable here —
to change it, edit the underlying records (gg record / gg reject / gg bug).`,
	RunE: runCanonSuggest,
}

var (
	canonApplyOpsPath string
	canonApplyDryRun  bool
)

var canonApplyCmd = &cobra.Command{
	Use:   "apply --ops <file|->",
	Short: "Apply a filled canon op contract (add/edit/delete curated areas) — deterministic, offline",
	RunE:  runCanonApply,
}

func init() {
	canonApplyCmd.Flags().StringVar(&canonApplyOpsPath, "ops", "", "path to a filled op contract JSON (or - for stdin)")
	canonApplyCmd.Flags().BoolVar(&canonApplyDryRun, "dry-run", false, "print the resolved ops without writing")
	canonCmd.AddCommand(canonSuggestCmd, canonApplyCmd)
}

func runCanonSuggest(cmd *cobra.Command, _ []string) error {
	d, err := loadDepsReadOnly(false)
	if err != nil {
		return err
	}
	defer d.Close()

	manual, err := d.store.ListCanon()
	if err != nil {
		return err
	}

	// Auto-derived canon + raw delta both need the ledger; degrade gracefully
	// when it is down (still emit curated + the op contract so the ritual works
	// offline against just the JSONL canon).
	var auto []store.CanonEntry
	var delta canonRawDelta
	if !d.qdrantDown {
		ctx, cancel := withTimeout(cmd.Context())
		defer cancel()
		auto = autoCanonEntries(ctx, d, false)
		delta = collectCanonRawDelta(ctx, d, manual)
	}

	contract := canonSuggestTemplate()

	return printJSON(map[string]any{
		"distilled":        map[string]any{"curated": manual, "auto_derived": auto},
		"raw_delta":        delta,
		"contract":         contract,
		"ledger_available": !d.qdrantDown,
	}, func() {
		writeCanonSuggest(cmd.OutOrStdout(), manual, auto, delta, contract, d.qdrantDown)
	})
}

// canonRawDelta holds the recent raw ledger entries not yet reflected in a
// curated canon area — the "distilled is missing this" signal.
type canonRawDelta struct {
	Decisions  []string `json:"decisions"`
	Rejections []string `json:"rejections"`
	Failures   []string `json:"failures"`
}

func (r canonRawDelta) empty() bool {
	return len(r.Decisions) == 0 && len(r.Rejections) == 0 && len(r.Failures) == 0
}

// collectCanonRawDelta returns recent decisions/rejections/fixed-bug root causes
// whose normalized text does not already appear in the curated canon body. This
// reuses the same normalizeForDedup key the auto-canon dedup uses, so "already
// reflected" is judged consistently. Auto-derived areas are intentionally NOT
// treated as "reflected": the point of the delta is to show what the *curated*
// (durable, agent-owned) layer is missing.
func collectCanonRawDelta(ctx context.Context, d *deps, curated []store.CanonEntry) canonRawDelta {
	reflected := map[string]bool{}
	for _, e := range curated {
		for _, line := range strings.Split(e.Text, "\n") {
			if k := store.NormalizeForDedup(line); k != "" {
				reflected[k] = true
			}
		}
	}

	var out canonRawDelta
	if decs, err := d.store.ListDecisions(ctx, 0, false); err == nil {
		for _, dd := range decs {
			if dd.Status != "" && dd.Status != "active" {
				continue
			}
			line := dd.Text
			if dd.Reason != "" {
				line += " — " + dd.Reason
			}
			if appendIfNovel(&out.Decisions, reflected, dd.Text, "• "+line) && len(out.Decisions) >= canonRawDeltaCap {
				break
			}
		}
	}
	if rejs, err := d.store.ListRejections(ctx, 0); err == nil {
		for _, r := range rejs {
			line := r.Approach
			if r.Reason != "" {
				line += " — " + r.Reason
			}
			if appendIfNovel(&out.Rejections, reflected, r.Approach, "✗ "+line) && len(out.Rejections) >= canonRawDeltaCap {
				break
			}
		}
	}
	if bugs, err := d.store.ListBugs(ctx, "fixed"); err == nil {
		for _, b := range bugs {
			if strings.TrimSpace(b.RootCause) == "" {
				continue
			}
			line := fmt.Sprintf("%s: %s [root cause: %s]", b.ID, b.Title, b.RootCause)
			if appendIfNovel(&out.Failures, reflected, b.RootCause, "✓ "+line) && len(out.Failures) >= canonRawDeltaCap {
				break
			}
		}
	}
	return out
}

// appendIfNovel appends rendered to dst iff the dedup key of raw is not already
// reflected; it also marks the key reflected so duplicate raw rows fold to one.
// Returns true when it appended (so callers can apply the per-kind cap).
func appendIfNovel(dst *[]string, reflected map[string]bool, raw, rendered string) bool {
	k := store.NormalizeForDedup(raw)
	if k == "" || reflected[k] {
		return false
	}
	reflected[k] = true
	*dst = append(*dst, rendered)
	return true
}

// canonSuggestTemplate returns the structured op contract the agent fills. It is
// a worked example (two ops), NOT a live suggestion — gg does no inference. The
// agent replaces it with real ops derived from the DISTILLED + RAW DELTA blocks.
func canonSuggestTemplate() canonSuggestContract {
	return canonSuggestContract{Operations: []canonSuggestOp{
		{
			Action:    "add",
			Area:      "architecture",
			Type:      "invariant",
			Text:      "<one durable sentence the next agent must know>",
			Tags:      []string{"<tag>", "<tag>"},
			DedupHint: "",
		},
		{
			Action:    "edit",
			Area:      "gotchas",
			Type:      "failure-mode",
			Text:      "<replacement text for an existing curated area that is now outdated>",
			DedupHint: "<existing curated area or raw line this supersedes>",
		},
	}}
}

func writeCanonSuggest(w io.Writer, manual, auto []store.CanonEntry, delta canonRawDelta, contract canonSuggestContract, ledgerDown bool) {
	fmt.Fprintln(w, "=== CANON CONSOLIDATION (no LLM — you distill; gg only emits the packet) ===")
	if ledgerDown {
		fmt.Fprintln(w, "⚠ ledger/vector store unreachable — RAW DELTA + auto-derived omitted; curated canon + contract still shown.")
	}

	// 1. DISTILLED — what the agent already inherits.
	fmt.Fprintln(w, "\n--- DISTILLED (current canon) ---")
	if len(manual) == 0 && len(auto) == 0 {
		fmt.Fprintln(w, "  (none yet)")
	}
	for _, e := range manual {
		fmt.Fprintf(w, "  [curated] %s: %s\n", e.Area, truncSuggest(canonOneLine(e.Text)))
	}
	for _, e := range auto {
		fmt.Fprintf(w, "  [auto]    %s: %s\n", e.Area, truncSuggest(canonOneLine(e.Text)))
	}

	// 2. RAW DELTA — what the curated layer has NOT yet captured.
	fmt.Fprintln(w, "\n--- RAW DELTA (recent ledger entries not yet in a curated area) ---")
	if delta.empty() {
		fmt.Fprintln(w, "  (curated canon is in sync with the recent ledger)")
	}
	writeDeltaSection(w, "decisions", delta.Decisions)
	writeDeltaSection(w, "rejections", delta.Rejections)
	writeDeltaSection(w, "fixed-bug root causes", delta.Failures)

	// 3. OP CONTRACT — the structured template the agent fills.
	fmt.Fprintln(w, "\n--- OP CONTRACT (fill, then `gg canon apply --ops`) ---")
	fmt.Fprintln(w, "  actions: add | edit | delete   types: decision | constraint | invariant | failure-mode | rejection")
	fmt.Fprintln(w, "  Copy the JSON below (or `gg canon suggest --json`), edit \"operations\", apply it:")
	fmt.Fprintln(w)
	for _, line := range strings.Split(marshalContract(contract), "\n") {
		fmt.Fprintf(w, "  %s\n", line)
	}
	fmt.Fprintln(w, "\n  Apply paths (curated layer is editable; auto-derived is a live digest, edit records instead):")
	fmt.Fprintln(w, "    gg canon suggest --json > ops.json && gg canon apply --ops ops.json")
	fmt.Fprintln(w, "    # or run directly:  gg canon set <area> \"<distilled text>\"   (delete: gg canon set <area> \"\")")
}

func writeDeltaSection(w io.Writer, label string, lines []string) {
	if len(lines) == 0 {
		return
	}
	fmt.Fprintf(w, "  %s (%d):\n", label, len(lines))
	for _, l := range lines {
		fmt.Fprintf(w, "    %s\n", truncSuggest(canonOneLine(l)))
	}
}

func truncSuggest(s string) string {
	const w = 200
	r := []rune(s)
	if len(r) <= w {
		return s
	}
	return strings.TrimSpace(string(r[:w])) + "…"
}

func runCanonApply(cmd *cobra.Command, _ []string) error {
	if strings.TrimSpace(canonApplyOpsPath) == "" {
		return fmt.Errorf("--ops <file|-> is required (a filled contract from `gg canon suggest --json`)")
	}
	raw, err := readCanonOps(cmd, canonApplyOpsPath)
	if err != nil {
		return err
	}
	contract, err := parseCanonContract(raw)
	if err != nil {
		return err
	}
	if len(contract.Operations) == 0 {
		return fmt.Errorf("contract has no operations")
	}

	d, err := loadDepsReadOnly(false)
	if err != nil {
		return err
	}
	defer d.Close()

	author := resolveAuthor(cmd)
	var applied, cleared int
	w := cmd.OutOrStdout()
	for i, op := range contract.Operations {
		area := strings.TrimSpace(op.Area)
		if area == "" {
			return fmt.Errorf("operations[%d]: area is required", i)
		}
		action := strings.ToLower(strings.TrimSpace(op.Action))
		switch action {
		case "add", "edit":
			text := strings.TrimSpace(op.Text)
			if text == "" {
				return fmt.Errorf("operations[%d] (%s %s): text is required", i, action, area)
			}
			if canonApplyDryRun {
				fmt.Fprintf(w, "would %-6s %s: %s\n", action, area, truncSuggest(text))
				continue
			}
			if err := d.store.SetCanon(area, text, author); err != nil {
				return fmt.Errorf("operations[%d] (%s %s): %w", i, action, area, err)
			}
			applied++
		case "delete":
			// Curated canon clears an area by writing empty text (last-write-wins).
			if canonApplyDryRun {
				fmt.Fprintf(w, "would delete %s\n", area)
				continue
			}
			if err := d.store.SetCanon(area, "", author); err != nil {
				return fmt.Errorf("operations[%d] (delete %s): %w", i, area, err)
			}
			cleared++
		default:
			return fmt.Errorf("operations[%d]: unknown action %q (want add|edit|delete)", i, op.Action)
		}
	}
	if canonApplyDryRun {
		fmt.Fprintln(w, "dry-run — no changes written")
		return nil
	}
	fmt.Fprintf(w, "✓ canon updated: %d set, %d cleared\n", applied, cleared)
	fmt.Fprintln(w, "  New agents receive this at session-start (gg canon show).")
	return nil
}

// readCanonOps reads the contract bytes from a file or stdin ("-").
func readCanonOps(cmd *cobra.Command, path string) ([]byte, error) {
	if path == "-" {
		return io.ReadAll(cmd.InOrStdin())
	}
	b, err := os.ReadFile(path) //nolint:gosec // path is an explicit operator-supplied arg
	if err != nil {
		return nil, fmt.Errorf("read ops: %w", err)
	}
	return b, nil
}

// marshalContract renders the op contract as indented JSON for the human packet.
// HTML escaping is disabled so the placeholder <…> markers render literally
// instead of as <, keeping the copy-paste template readable.
func marshalContract(c canonSuggestContract) string {
	var sb strings.Builder
	enc := json.NewEncoder(&sb)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(c); err != nil {
		return "{\"operations\": []}"
	}
	return strings.TrimRight(sb.String(), "\n")
}

// parseCanonContract decodes a filled op contract. It tolerates a bare
// operations array as well as the wrapped {"operations": [...]} object.
func parseCanonContract(raw []byte) (canonSuggestContract, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return canonSuggestContract{}, fmt.Errorf("empty ops contract")
	}
	if strings.HasPrefix(trimmed, "[") {
		var ops []canonSuggestOp
		if err := json.Unmarshal([]byte(trimmed), &ops); err != nil {
			return canonSuggestContract{}, fmt.Errorf("parse ops array: %w", err)
		}
		return canonSuggestContract{Operations: ops}, nil
	}
	var c canonSuggestContract
	if err := json.Unmarshal([]byte(trimmed), &c); err != nil {
		return canonSuggestContract{}, fmt.Errorf("parse ops contract: %w", err)
	}
	return c, nil
}
