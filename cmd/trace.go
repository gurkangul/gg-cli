package cmd

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/trace"
)

var traceCmd = &cobra.Command{
	Use:   "trace",
	Short: "Inspect GG_TRACE span data",
	Long: `Commands for reading, summarising, and clearing trace spans.

Trace recording is enabled by setting GG_TRACE=1 before running gg commands.
Each operation is appended as a JSON line to .gg/traces/YYYY-MM-DD.jsonl.`,
}

// ── trace show ────────────────────────────────────────────────────────────────

var traceShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Print recorded spans",
	Long:  `Prints spans in reverse-chronological order (newest first). Filter by op name, time window, and result count.`,
	Args:  cobra.NoArgs,
	RunE:  runTraceShow,
}

var (
	traceShowOp    string
	traceShowSince string
	traceShowLimit int
)

func init() {
	traceShowCmd.Flags().StringVar(&traceShowOp, "op", "", "filter by operation name (e.g. search, record, index)")
	traceShowCmd.Flags().StringVar(&traceShowSince, "since", "", "only show spans newer than this duration (e.g. 1h, 2d, 30m)")
	traceShowCmd.Flags().IntVar(&traceShowLimit, "limit", 50, "max spans to show (0 = all)")
	traceCmd.AddCommand(traceShowCmd)
}

func runTraceShow(cmd *cobra.Command, _ []string) error {
	ggDir, err := config.GGDir()
	if err != nil {
		return err
	}

	since, err := parseSinceDuration(traceShowSince)
	if err != nil {
		return fmt.Errorf("--since: %w", err)
	}

	spans, err := trace.ReadSpansFromDir(ggDir, traceShowOp, since, traceShowLimit)
	if err != nil {
		return fmt.Errorf("read traces: %w", err)
	}

	if len(spans) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No trace spans found.")
		return nil
	}

	// Show newest first.
	for i, j := 0, len(spans)-1; i < j; i, j = i+1, j-1 {
		spans[i], spans[j] = spans[j], spans[i]
	}

	type row struct {
		ts  string
		op  string
		dur string
		err string
	}

	rows := make([]row, 0, len(spans))
	for _, s := range spans {
		ts := time.Unix(s.Timestamp, 0).Local().Format("2006-01-02 15:04:05")
		errMark := ""
		if s.IsErr {
			errMark = "✗"
		}
		rows = append(rows, row{ts: ts, op: s.Op, dur: trace.FmtMs(s.DurationMs), err: errMark})
	}

	return printJSON(map[string]any{"spans": spans}, func() {
		fmt.Fprintf(cmd.OutOrStdout(), "%-20s  %-30s  %9s  %s\n", "TIMESTAMP", "OP", "DURATION", "ERR")
		fmt.Fprintln(cmd.OutOrStdout(), strings.Repeat("─", 66))
		for _, r := range rows {
			fmt.Fprintf(cmd.OutOrStdout(), "%-20s  %-30s  %9s  %s\n", r.ts, r.op, r.dur, r.err)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "\n%d span(s)\n", len(rows))
	})
}

// ── trace summary ─────────────────────────────────────────────────────────────

var traceSummaryCmd = &cobra.Command{
	Use:   "summary",
	Short: "Per-operation latency breakdown (p50/p95/p99)",
	Long:  `Groups all recorded spans by operation and shows p50/p95/p99 latencies and sample count.`,
	Args:  cobra.NoArgs,
	RunE:  runTraceSummary,
}

var traceSummarySince string

func init() {
	traceSummaryCmd.Flags().StringVar(&traceSummarySince, "since", "", "only include spans newer than this duration (e.g. 1h, 2d)")
	traceCmd.AddCommand(traceSummaryCmd)
}

func runTraceSummary(cmd *cobra.Command, _ []string) error {
	ggDir, err := config.GGDir()
	if err != nil {
		return err
	}

	since, err := parseSinceDuration(traceSummarySince)
	if err != nil {
		return fmt.Errorf("--since: %w", err)
	}

	summary, err := trace.SummaryByOp(ggDir, since)
	if err != nil {
		return fmt.Errorf("read traces: %w", err)
	}

	if len(summary) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No trace data found.")
		return nil
	}

	// Sort ops alphabetically for stable output.
	ops := make([]string, 0, len(summary))
	for op := range summary {
		ops = append(ops, op)
	}
	sort.Strings(ops)

	return printJSON(summary, func() {
		fmt.Fprintf(cmd.OutOrStdout(), "%-30s  %9s  %9s  %9s  %6s\n", "OP", "P50", "P95", "P99", "N")
		fmt.Fprintln(cmd.OutOrStdout(), strings.Repeat("─", 70))
		for _, op := range ops {
			p := summary[op]
			fmt.Fprintf(cmd.OutOrStdout(), "%-30s  %9s  %9s  %9s  %6d\n",
				op,
				trace.FmtMs(p.P50),
				trace.FmtMs(p.P95),
				trace.FmtMs(p.P99),
				p.N,
			)
		}
	})
}

// ── trace clear ───────────────────────────────────────────────────────────────

var traceClearCmd = &cobra.Command{
	Use:   "clear",
	Short: "Delete old trace files",
	Long:  `Deletes .gg/traces/*.jsonl files older than the given duration.`,
	Args:  cobra.NoArgs,
	RunE:  runTraceClear,
}

var traceClearOlderThan string

func init() {
	traceClearCmd.Flags().StringVar(&traceClearOlderThan, "older-than", "7d", "delete files older than this duration (e.g. 7d, 30d)")
	traceCmd.AddCommand(traceClearCmd)
	rootCmd.AddCommand(traceCmd)
}

func runTraceClear(cmd *cobra.Command, _ []string) error {
	ggDir, err := config.GGDir()
	if err != nil {
		return err
	}

	dur, err := parseDuration(traceClearOlderThan)
	if err != nil {
		return fmt.Errorf("--older-than: %w", err)
	}
	cutoff := time.Now().Add(-dur)

	deleted, err := trace.ClearOlderThan(ggDir, cutoff)
	if err != nil {
		return fmt.Errorf("clear traces: %w", err)
	}

	return printJSON(map[string]any{"deleted": deleted}, func() {
		if deleted == 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "No trace files to delete.")
		} else {
			fmt.Fprintf(cmd.OutOrStdout(), "Deleted %d trace file(s) older than %s.\n", deleted, traceClearOlderThan)
		}
	})
}

// parseSinceDuration parses a --since flag value into an absolute time.
// Empty string returns a zero time (no filter).
func parseSinceDuration(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	dur, err := parseDuration(s)
	if err != nil {
		return time.Time{}, err
	}
	return time.Now().Add(-dur), nil
}
