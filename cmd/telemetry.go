package cmd

import (
	"fmt"
	"sort"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/telemetry"
)

var telemetryCmd = &cobra.Command{
	Use:   "telemetry",
	Short: "Manage local usage telemetry",
	Long: `Local-only, PII-free usage telemetry. Opt-in — disabled by default.

Enable in your project config:
  gg config set telemetry.enabled true

Or via environment variable (temporary):
  GG_TELEMETRY=1 gg status

Disable permanently:
  gg config set telemetry.enabled false`,
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

func runTelemetrySummary(cmd *cobra.Command, _ []string) error {
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

		fmt.Println("Command usage:")
		for _, v := range verbs {
			bar := ""
			for i := 0; i < v.count && i < 20; i++ {
				bar += "█"
			}
			fmt.Printf("  %-16s %3d  %s\n", v.verb, v.count, bar)
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
