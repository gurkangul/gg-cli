package cmd

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/config"
)

var graphStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "One-line typed CodeGraph freshness an agent can read",
	Long: `Print the CodeGraph freshness as a single typed line:

  codegraph: fresh|stale(reason)|empty|... | idx=<sha> head=<sha> | files=N sym=M edges=K

This is the agent-facing readiness signal for the local code graph. It reuses the
same freshness contract as 'gg index status' (no duplicate logic); use --compact
for the dense one-line form and the default render for a fuller breakdown.

gg never refreshes the graph in the background — when the line reports stale or
empty, repair is explicit (gg doctor --fix-index or gg index --lang <lang>).`,
	Args: cobra.NoArgs,
	RunE: runGraphStatus,
}

var graphStatusCompact bool

func init() {
	graphStatusCmd.Flags().BoolVar(&graphStatusCompact, "compact", false, "print the dense one-line typed freshness signal")
	graphCmd.AddCommand(graphStatusCmd)
}

func runGraphStatus(cmd *cobra.Command, _ []string) error {
	cfg, err := config.Load()
	if err != nil {
		return configErr(fmt.Sprintf("load config: %v", err))
	}
	root, err := config.FindRoot()
	if err != nil {
		return configErr(err.Error())
	}
	ggDir := root + "/" + config.DirName

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	status := collectCodeGraphStatus(ctx, root, ggDir, cfg)
	return printJSON(status, func() {
		if isCompactActive(cmd) {
			fmt.Fprintln(cmd.OutOrStdout(), codeGraphTypedStatusLine(status))
			return
		}
		renderCodeGraphStatus(cmd.OutOrStdout(), status)
	})
}

// codeGraphTypedStatusLine renders the single typed freshness line agents read:
//
//	codegraph: <state>[(<reason>)] | idx=<sha> head=<sha> | files=N sym=M edges=K
//
// where <state> is fresh|stale|empty|missing|unavailable|unknown|n/a, derived
// from the shared freshness contract (no duplicate freshness logic). A reason is
// appended in parentheses for any non-fresh state so the agent can branch on it.
func codeGraphTypedStatusLine(s codeGraphStatus) string {
	fresh := s.freshnessContract()
	state := codeGraphTypedState(s, fresh)
	head := codeGraphTypedField(state)
	if fresh.Reason != "" && state != "fresh" && state != "n/a" {
		head += "(" + fresh.Reason + ")"
	}
	parts := []string{"codegraph: " + head}
	parts = append(parts, fmt.Sprintf("idx=%s head=%s", indexStatusShortSHA(s.LastIndexedSHA), indexStatusShortSHA(s.HeadSHA)))
	parts = append(parts, fmt.Sprintf("files=%d sym=%d edges=%d", s.Stats.Files, s.Stats.Symbols, s.Stats.Edges))
	return strings.Join(parts, " | ")
}

// codeGraphTypedState collapses the freshness-contract status into the compact
// vocabulary the typed line uses. "empty" is surfaced distinctly from "missing"
// when the graph is reachable but holds nothing, so the agent can tell "never
// built" from "built then drifted".
func codeGraphTypedState(s codeGraphStatus, fresh codeGraphFreshness) string {
	switch fresh.Status {
	case codeGraphFreshnessReady:
		return "fresh"
	case codeGraphFreshnessStale:
		return "stale"
	case codeGraphFreshnessMissing:
		if s.GraphAvailable && s.GraphEmpty {
			return "empty"
		}
		return "missing"
	case codeGraphFreshnessUnavailable:
		return "unavailable"
	case codeGraphFreshnessNotApplicable:
		return "n/a"
	case codeGraphFreshnessUnknown:
		return "unknown"
	default:
		if fresh.Status == "" {
			return "unknown"
		}
		return fresh.Status
	}
}

func codeGraphTypedField(state string) string {
	if state == "" {
		return "unknown"
	}
	return state
}

// graphStatusStderrNotice returns the one-line stderr notice that search/impact
// emit when the graph is stale or empty (TASK-504): a terse signal that the
// graph needs a refresh, distinct from the verbose CODE GRAPH NOTICE block. It
// is empty when the graph is fresh/not-applicable (no notice needed).
func graphStatusStderrNotice(s codeGraphStatus) string {
	fresh := s.freshnessContract()
	if !fresh.NeedsNotice() {
		return ""
	}
	line := codeGraphTypedStatusLine(s)
	if fresh.SuggestedCommand != "" {
		line += " — run: " + fresh.SuggestedCommand
	}
	return line
}

// emitGraphStatusNotice writes the one-line stale/empty graph notice to w when
// applicable. Callers pass os.Stderr (or cmd.OutOrStderr) so the notice never
// pollutes the parseable stdout payload.
func emitGraphStatusNotice(w io.Writer, s codeGraphStatus) {
	if notice := graphStatusStderrNotice(s); notice != "" {
		fmt.Fprintln(w, notice)
	}
}
