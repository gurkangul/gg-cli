package cmd

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/telemetry"
)

// telemetryBarWidth is the maximum bar length, in cells, drawn for the
// busiest verb. Every other verb's bar is scaled proportionally to it.
const telemetryBarWidth = 20

var telemetryCmd = &cobra.Command{
	Use:   "telemetry",
	Short: experimentalShort("Manage local usage telemetry"),
	Long: experimentalLong(`Local-only, PII-free usage telemetry. Opt-in — disabled by default.

Enable in your project config:
  gg config set telemetry.enabled true

Or via environment variable (temporary):
  GG_TELEMETRY=1 gg status

Disable permanently:
  gg config set telemetry.enabled false`),
	// BUG-093: bare `gg telemetry` defaults to the summary view instead of
	// printing the experimental help blurb. An unknown subcommand (e.g.
	// `gg telemetry frob`) reaches this RunE with a non-empty args slice and is
	// rejected there rather than silently falling through to the summary.
	RunE: runTelemetrySummary,
}

// telemetryBar renders a proportional bar: its length is count/max of
// telemetryBarWidth cells, so the busiest verb fills the bar and the rest
// scale down from it. A non-zero count always shows at least one cell.
func telemetryBar(count, max int) string {
	if count <= 0 || max <= 0 {
		return ""
	}
	width := (count*telemetryBarWidth + max/2) / max // round to nearest
	if width < 1 {
		width = 1
	}
	if width > telemetryBarWidth {
		width = telemetryBarWidth
	}
	return strings.Repeat("█", width)
}

// topLevelVerbs returns the set of verbs that are runnable as `gg <verb>`:
// every registered top-level command name plus its aliases. Used by the
// telemetry summary to separate real commands from brain-kind / subcommand
// access labels (BUG-092b).
func topLevelVerbs() map[string]bool {
	out := map[string]bool{}
	for _, c := range rootCmd.Commands() {
		out[c.Name()] = true
		for _, a := range c.Aliases {
			out[a] = true
		}
	}
	return out
}

var telemetrySummaryCmd = &cobra.Command{
	Use:   "summary",
	Short: "Show command usage summary for the last 7 days",
	RunE:  runTelemetrySummary,
}

var telemetryCompactMissedCmd = &cobra.Command{
	Use:   "compact-missed",
	Short: "Show per-verb missed compact savings (last 7 days, agent-origin only)",
	Long: `For each verb that has at least one compact-mode call (i.e. the
command has a working compact render path), report how many agent-origin calls
still ran default and the estimated bytes/tokens that would have been saved if
those calls had used --compact. Human full-reads are excluded — they are not
missed compact opportunities. The estimate is per-verb and conservative: it
uses each verb's own observed avg-bytes-saved-per-compact-call.`,
	RunE: runTelemetryCompactMissed,
}

func init() {
	telemetryCmd.AddCommand(telemetrySummaryCmd)
	telemetryCmd.AddCommand(telemetryCompactMissedCmd)
	rootCmd.AddCommand(telemetryCmd)
}

func runTelemetrySummary(cmd *cobra.Command, args []string) error {
	// BUG-093: bare `gg telemetry` shows the summary, but an unknown subcommand
	// (e.g. `gg telemetry frob`) must not silently fall through to it. Any
	// positional arg here is an unrecognised subcommand — reject it. `summary`
	// itself takes no positional args, so this guard is also correct for the
	// `gg telemetry summary <extra>` form.
	if len(args) > 0 {
		return fmt.Errorf("unknown telemetry subcommand %q — run `gg telemetry --help` to list subcommands (bare `gg telemetry` shows the summary)", args[0])
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	runtimeDir, err := cfg.RuntimeDir()
	if err != nil {
		return fmt.Errorf("runtime dir: %w", err)
	}

	sum, err := telemetry.Summarize(runtimeDir)
	if err != nil {
		return fmt.Errorf("summarize: %w", err)
	}

	if telemetry.IsDisabled() {
		fmt.Fprintln(cmd.ErrOrStderr(), "warning: Telemetry is disabled — no data collected.")
		fmt.Fprintln(cmd.ErrOrStderr(), "  Re-enable by unsetting GG_TELEMETRY=0 or removing telemetry.enabled: false from .gg/config.yaml.")
		return nil
	}

	return printJSON(map[string]any{
		"total":                       sum.Total,
		"agent_calls":                 sum.AgentCalls,
		"human_calls":                 sum.HumanCalls,
		"verb_counts":                 sum.VerbCounts,
		"hydration_calls":             sum.HydrationCalls,
		"hydration_verb_counts":       sum.HydrationVerbCounts,
		"net_savings_bytes":           sum.NetSavingsBytes,
		"missing_handler_calls":       sum.MissingHandlerCalls,
		"missing_handler_verb_counts": sum.MissingHandlerVerbCounts,
	}, func() {
		if sum.Total == 0 {
			fmt.Println("No telemetry recorded in the last 7 days.")
			return
		}

		fmt.Printf("Last 7 days: %d calls (%d human, %d agent)\n\n",
			sum.Total, sum.HumanCalls, sum.AgentCalls)

		// Sort verbs by count descending.
		type vc struct {
			verb  string
			count int
		}
		var verbs []vc
		for v, c := range sum.VerbCounts {
			verbs = append(verbs, vc{v, c})
		}
		sort.Slice(verbs, func(i, j int) bool {
			if verbs[i].count != verbs[j].count {
				return verbs[i].count > verbs[j].count
			}
			return verbs[i].verb < verbs[j].verb
		})

		// BUG-092(a): scale each bar proportionally to the busiest verb so a
		// 903-count verb and a 75-count verb no longer both render full-width.
		maxCount := 0
		for _, v := range verbs {
			if v.count > maxCount {
				maxCount = v.count
			}
		}
		// BUG-092(b): split runnable top-level commands from brain-kind / sub-
		// command access labels so an agent does not read "decisions" or "get"
		// as a runnable `gg <verb>`. A verb is a real command only when it is a
		// registered top-level command (or alias) on the root.
		topLevel := topLevelVerbs()
		var cmdRows, brainRows []vc
		for _, v := range verbs {
			if topLevel[v.verb] {
				cmdRows = append(cmdRows, v)
			} else {
				brainRows = append(brainRows, v)
			}
		}

		printVerbRow := func(v vc, suffix string) {
			fmt.Printf("  %-16s %3d  %s%s\n", v.verb, v.count, telemetryBar(v.count, maxCount), suffix)
		}

		fmt.Println("Command usage:")
		for _, v := range cmdRows {
			printVerbRow(v, "")
		}
		if len(brainRows) > 0 {
			fmt.Println("\nSubcommand verbs (leaf verbs like `gg task get`, `gg telemetry summary` — not top-level `gg <verb>`):")
			for _, v := range brainRows {
				printVerbRow(v, "  (subcommand)")
			}
		}

		if sum.CompactCalls > 0 {
			saved := sum.CompactBytesDefault - sum.CompactBytesOut
			pct := 0
			if sum.CompactBytesDefault > 0 {
				pct = 100 * saved / sum.CompactBytesDefault
			}
			fmt.Printf("\n--compact: %d calls saved %d bytes / ~%s tok (est. calibrated: %d bytes/tok) (%d%%)\n",
				sum.CompactCalls, saved, humanTokenCount(sum.CompactTokensSaved), telemetry.CorpusCalibration.Rounded, pct)
		}
		if sum.HydrationCalls > 0 {
			netStr := ""
			if sum.NetSavingsBytes >= 0 {
				netStr = fmt.Sprintf(", net %s saved", humanFileSize(int64(sum.NetSavingsBytes)))
			} else {
				netStr = fmt.Sprintf(", net %s overfetch", humanFileSize(int64(-sum.NetSavingsBytes)))
			}
			refetchPct := 0
			discretionaryPct := 0
			if sum.CompactCalls > 0 {
				refetchPct = 100 * sum.HydrationCalls / sum.CompactCalls
				discretionaryPct = 100 * sum.AgentDiscretionaryHydration / sum.CompactCalls
			}
			fmt.Printf("re-fetch: %d calls (%d%% of compact; %d mandated by gate / %d discretionary%s)",
				sum.HydrationCalls, refetchPct,
				sum.AgentMandatedHydrationCalls, sum.AgentDiscretionaryHydration, netStr)
			if len(sum.HydrationVerbCounts) > 0 {
				var hvc []vc
				for v, c := range sum.HydrationVerbCounts {
					hvc = append(hvc, vc{v, c})
				}
				sort.Slice(hvc, func(i, j int) bool {
					if hvc[i].count != hvc[j].count {
						return hvc[i].count > hvc[j].count
					}
					return hvc[i].verb < hvc[j].verb
				})
				fmt.Printf("  (")
				for i, v := range hvc {
					if i > 0 {
						fmt.Printf(", ")
					}
					fmt.Printf("%s=%d", v.verb, v.count)
				}
				fmt.Printf(")")
			}
			fmt.Println()
			// TASK-491: warn only on the DISCRETIONARY agent rate. Gate-mandated
			// --full reads (hydration gate / bug triage) and human full-reads are
			// reported in the split but never trigger the drop-list warning — they
			// are required first reads, not a compact-induced overfetch.
			if discretionaryPct > 50 {
				fmt.Printf("  warning: discretionary re-fetch rate >50%% — compact drop-list may be dropping fields agents need\n")
			}
			if sum.AgentMandatedHydrationCalls > 0 {
				fmt.Printf("  note: %d mandated re-fetch(es) are required by the hydration/bug gates, not a compact problem\n",
					sum.AgentMandatedHydrationCalls)
			}
		}
		if sum.WithContextCalls > 0 {
			fmt.Printf("--with-context: %d calls, %d bytes total context\n",
				sum.WithContextCalls, sum.WithContextBytesTotal)
		}
		if sum.TaskStartContextCalls > 0 {
			fmt.Printf("task-start memory push: %d packets (%d delivered, %d empty, %d failed), %d bytes total context\n",
				sum.TaskStartContextCalls, sum.TaskStartContextDelivered,
				sum.TaskStartContextEmpty, sum.TaskStartContextFailed,
				sum.TaskStartContextBytesTotal)
			if unknown := sum.TaskStartContextCalls - sum.TaskStartContextDelivered -
				sum.TaskStartContextEmpty - sum.TaskStartContextFailed; unknown > 0 {
				fmt.Printf("  note: %d packet(s) predate the delivery split and are excluded from it\n", unknown)
			}
			// Only a FAILED packet indicts the backend. An empty one usually just
			// means the project has not recorded anything on that topic yet, and
			// sending someone to debug a healthy embedding backend over it is
			// worse than saying nothing. Judged excludes pre-split entries so the
			// warning can never fire on backfill alone.
			judged := sum.TaskStartContextDelivered + sum.TaskStartContextEmpty + sum.TaskStartContextFailed
			if judged > 0 && sum.TaskStartContextFailed*2 > judged {
				fmt.Printf("  warning: over half of pushed packets failed to look — check the embedding backend and vector store\n")
			}
		}
		if sum.DupeCheckCalls > 0 {
			fmt.Printf("dupe-check: %d calls, %d fires  (cancel=%d force=%d auto-force=%d reuse=%d)\n",
				sum.DupeCheckCalls, sum.DupeCheckMatchesHits,
				sum.DupeChoiceCancel, sum.DupeChoiceForce,
				sum.DupeChoiceAutoForce, sum.DupeChoiceReuse)
		}
		if sum.MissingHandlerCalls > 0 {
			type vc struct {
				verb  string
				count int
			}
			var verbs []vc
			for v, c := range sum.MissingHandlerVerbCounts {
				verbs = append(verbs, vc{v, c})
			}
			sort.Slice(verbs, func(i, j int) bool {
				if verbs[i].count != verbs[j].count {
					return verbs[i].count > verbs[j].count
				}
				return verbs[i].verb < verbs[j].verb
			})
			fmt.Printf("missing compact handler: %d calls", sum.MissingHandlerCalls)
			fmt.Printf("  (")
			for i, v := range verbs {
				if i > 0 {
					fmt.Printf(", ")
				}
				fmt.Printf("%s=%d", v.verb, v.count)
			}
			fmt.Printf(")\n")
			fmt.Printf("  warning: these verbs have compact active but no compact render path — add emitCompact\n")
		}
	})
}

func runTelemetryCompactMissed(cmd *cobra.Command, _ []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	runtimeDir, err := cfg.RuntimeDir()
	if err != nil {
		return fmt.Errorf("runtime dir: %w", err)
	}
	sum, err := telemetry.Summarize(runtimeDir)
	if err != nil {
		return fmt.Errorf("summarize: %w", err)
	}
	if telemetry.IsDisabled() {
		fmt.Fprintln(cmd.ErrOrStderr(), "warning: Telemetry is disabled — no data collected.")
		return nil
	}
	rows := sum.MissedCompactByVerb(0)
	if len(rows) == 0 {
		fmt.Println("No missed compact-savings detected in the last 7 days.")
		fmt.Println("(A verb appears here only after at least one --compact call has measured per-call savings for it.)")
		return nil
	}
	fmt.Printf("Missed compact savings — last 7 days (%d verbs, agent-origin calls only):\n", len(rows))
	fmt.Printf("  %-16s %10s %10s %12s %12s\n", "verb", "missed", "agent-total", "avg-saved", "est. missed")
	for _, r := range rows {
		estTok := r.EstimatedBytesMissed / telemetry.BytesPerToken
		fmt.Printf("  %-16s %10d %10d %12s %s / ~%s tok\n",
			r.Verb, r.MissedCalls, r.TotalCalls,
			humanFileSize(int64(r.AvgBytesSavedPerCall)),
			humanFileSize(int64(r.EstimatedBytesMissed)),
			humanTokenCount(estTok))
	}
	fmt.Println("\nEstimates are conservative: they multiply agent-origin missed-call counts by each verb's own observed avg-bytes-saved-per-compact-call.")
	fmt.Println("Human full-reads are excluded from missed counts — only agent-origin calls are expected to use --compact.")
	return nil
}
