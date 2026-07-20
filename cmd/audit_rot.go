package cmd

import (
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/brain"
	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/store"
)

// audit_rot.go — TASK-519 read-only sweep for ledger rot.
//
// The trust tier (trust.go) makes decay visible when you happen to READ a
// decision. That is not enough on its own: the entries most likely to mislead
// are the ones nobody reads. This sweep inverts that — it goes looking.
//
// Three kinds of rot, all reported, NOTHING mutated. The judges rated automatic
// correction of canonical state the highest blast-radius risk in this roadmap,
// so this command is deliberately a report. It does not supersede, retag, or
// rewrite anything, and it exits 0 whatever it finds: it is a lens, not a gate.
//
// It reads the JSONL ledger directly, so it works with the vector store and the
// code graph both offline.

var (
	auditRotCompact      bool
	auditRotIncludeAging bool
	auditRotLimit        int
)

var auditRotCmd = &cobra.Command{
	Use:   "rot",
	Short: "Report decaying ledger entries: stale evidence, unproven load-bearing rules, orphans",
	Long: `Sweep the decision ledger for entries that have quietly gone bad.

Reports three kinds of rot:
  stale       evidence-backed decisions whose verification is old enough that it
              should be re-checked before being leaned on
  unproven    pinned or policy-tagged decisions carrying NO evidence — the most
              load-bearing entries in the ledger, never actually verified
  orphan      active decisions with no link in either direction, reachable only
              by search and never by walking the graph

Read-only: nothing is superseded, retagged, or rewritten, and the command always
exits 0. Pins and constraint/convention/policy tags are exempt from staleness —
a recorded rule is not a measurement that expires.

See also: gg backlinks (who links here), gg related (walk the graph)`,
	Args: cobra.NoArgs,
	RunE: runAuditRot,
}

func init() {
	auditRotCmd.Flags().BoolVar(&auditRotCompact, "compact", false, "one line per entry — preserves agent context window")
	auditRotCmd.Flags().BoolVar(&auditRotIncludeAging, "include-aging", false, "also report decisions that are aging but not yet stale")
	auditRotCmd.Flags().IntVar(&auditRotLimit, "limit", 15, "max entries to list per category (0 = no limit)")
	auditCmd.AddCommand(auditRotCmd)
}

// rotFinding is one flagged decision plus why it was flagged.
type rotFinding struct {
	Decision store.Decision
	Tier     string
}

type rotReport struct {
	Stale    []rotFinding
	Unproven []rotFinding
	Orphans  []rotFinding
	Scanned  int
}

func runAuditRot(cmd *cobra.Command, _ []string) error {
	ggDir := config.GGDirOrEmpty()
	if ggDir == "" {
		return configErr("not inside a gg project — run gg init first")
	}

	entries, err := brain.ReadLatest(ggDir, "decisions")
	if err != nil {
		return fmt.Errorf("read decisions: %w", err)
	}
	// Orphan detection needs the resolved graph; a failure here degrades that one
	// category rather than failing the whole sweep.
	graph, graphErr := brain.LoadLinkGraph(ggDir)

	now := time.Now()
	report := rotReport{}
	for _, e := range entries {
		d := decisionFromJSONLEntry(e)
		// Superseded and rejected decisions are already marked as not-current;
		// reporting their decay would be noise.
		if !hybridDecisionVisible(d.Status, false) {
			continue
		}
		report.Scanned++

		tier := trustTier(d, now)
		switch {
		case tier == trustStale, auditRotIncludeAging && tier == trustAging:
			report.Stale = append(report.Stale, rotFinding{Decision: d, Tier: tier})
		case tier == trustUnverified && trustExempt(d):
			// Pinned/policy entries are what every session inherits first. An
			// unverified one is the most consequential gap in the ledger.
			report.Unproven = append(report.Unproven, rotFinding{Decision: d, Tier: tier})
		}

		if graphErr == nil && graph.Degree(d.ID) == 0 {
			report.Orphans = append(report.Orphans, rotFinding{Decision: d, Tier: tier})
		}
	}

	jsonMap := map[string]any{
		"scanned":  report.Scanned,
		"stale":    rotJSON(report.Stale),
		"unproven": rotJSON(report.Unproven),
		"orphans":  rotJSON(report.Orphans),
	}
	return printJSON(jsonMap, func() {
		if isCompactActive(cmd) {
			emitCompact(cmd, "rot",
				func(w io.Writer) { renderRotDefault(w, report) },
				func(w io.Writer) { renderRotCompact(w, report) },
				compactRendererV_auditRot,
			)
			return
		}
		renderRotDefault(cmd.OutOrStdout(), report)
	})
}

func rotJSON(findings []rotFinding) []map[string]any {
	out := make([]map[string]any, 0, len(findings))
	for _, f := range findings {
		out = append(out, map[string]any{
			"id":         f.Decision.ID,
			"text":       f.Decision.Text,
			"tier":       f.Tier,
			"pinned":     f.Decision.Pinned,
			"tags":       f.Decision.Tags,
			"created_at": f.Decision.CreatedAt,
		})
	}
	return out
}

// rotCap applies --limit and reports how many were withheld, so a truncated
// list can never be mistaken for a complete one.
func rotCap(findings []rotFinding) ([]rotFinding, int) {
	if auditRotLimit <= 0 || len(findings) <= auditRotLimit {
		return findings, 0
	}
	return findings[:auditRotLimit], len(findings) - auditRotLimit
}

func renderRotSection(w io.Writer, title, remedy string, findings []rotFinding) {
	shown, withheld := rotCap(findings)
	fmt.Fprintf(w, "\n%s (%d)\n", title, len(findings))
	if len(findings) == 0 {
		fmt.Fprintln(w, "  none")
		return
	}
	for _, f := range shown {
		marker := ""
		if f.Decision.Pinned {
			marker = "📌 "
		}
		fmt.Fprintf(w, "  • %s[%s] %s\n", marker, shortDate(f.Decision.CreatedAt), compactTrim(f.Decision.Text, 90))
	}
	if withheld > 0 {
		fmt.Fprintf(w, "  … %d more (raise --limit to see them)\n", withheld)
	}
	fmt.Fprintf(w, "  → %s\n", remedy)
}

func renderRotDefault(w io.Writer, r rotReport) {
	fmt.Fprintf(w, "LEDGER ROT — %d active decision(s) scanned\n", r.Scanned)
	renderRotSection(w, "STALE EVIDENCE", "re-verify, then gg record the fresh evidence", r.Stale)
	renderRotSection(w, "UNPROVEN LOAD-BEARING", "attach evidence with gg record --evidence, or unpin", r.Unproven)
	renderRotSection(w, "ORPHANS", "link them: name the task/bug in the text, or gg record --implements", r.Orphans)
	fmt.Fprintln(w, "\n(read-only — nothing was changed)")
}

func renderRotCompact(w io.Writer, r rotReport) {
	fmt.Fprintf(w, "rot — %dS %dU %dO of %d scanned\n\n",
		len(r.Stale), len(r.Unproven), len(r.Orphans), r.Scanned)
	// Ordered explicitly: ranging a map would reorder the output run to run and
	// make the compact form useless to diff.
	sections := []struct {
		label    string
		findings []rotFinding
	}{
		{"S", r.Stale},
		{"U", r.Unproven},
		{"O", r.Orphans},
	}
	for _, s := range sections {
		shown, withheld := rotCap(s.findings)
		for _, f := range shown {
			fmt.Fprintf(w, "%s %s %s\n", s.label, shortDate(f.Decision.CreatedAt), compactTrim(f.Decision.Text, 70))
		}
		if withheld > 0 {
			fmt.Fprintf(w, "%s … %d more\n", s.label, withheld)
		}
	}
	if r.Scanned == 0 {
		fmt.Fprintln(w, "(no active decisions)")
	}
}
