package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/filesize"
)

var auditFileSizeCmd = &cobra.Command{
	Use:   "file-size",
	Short: "List source files violating the 500-line (800 for tests) size rule",
	Long: `Walk the project and report files that exceed the size limit.
Source files (.go/.ts/.js/.py/.rs/.java) must stay under 500 lines;
test files (*_test.go, *.test.*, *.spec.*) under 800 lines.

A file at or above 90% of its limit (450 source / 720 test) is also listed
under "approaching limit". Those are not violations and never affect the exit
code — they are the warning band that lets a file be split on the next touch
instead of at the wall.

Files in the .gg/file-size-baseline.json grandfather list are only
flagged when they have grown beyond their baseline value. The baseline does not
suppress the warning band: a grandfathered file is exempt from failing, not
from being visible.

Use --no-baseline to see raw violations ignoring the grandfather list.
Use --over N to report every file above N lines, replacing the per-type
defaults — this is also the machine-readable way to query the band
(--over 450 --json). --over is a raw size query and always ignores the
grandfather list, which only ever excuses violations of the real limits.
Use --json for machine-readable output (a bare array of violations).`,
	Args: cobra.NoArgs,
	RunE: runAuditFileSize,
}

// runAuditFileSize implements `gg audit file-size`.
func runAuditFileSize(cmd *cobra.Command, _ []string) error {
	w := cmd.OutOrStdout()
	root, err := os.Getwd()
	if err != nil {
		return err
	}

	allFiles, err := filesize.ScanDir(root)
	if err != nil {
		return fmt.Errorf("scanning project: %w", err)
	}

	// An explicit --over is a raw query about file sizes, not an evaluation of
	// the rule, so the grandfather list must not filter it: cmd/audit.go is
	// baselined at 843 and is 211 lines today, and "--over 100" silently omitted
	// it. Suppressing files from an arbitrary threshold query using a list that
	// exists to excuse rule violations is the same silent-omission failure this
	// command was fixed to remove.
	var b *filesize.Baseline
	useBaseline := !auditFileSizeNoBaseline && auditFileSizeOver <= 0
	if useBaseline {
		b, err = filesize.ReadBaseline(root)
		if err != nil {
			return fmt.Errorf("reading baseline: %w", err)
		}
	}

	// BUG-107: --over is a real threshold that replaces the per-type defaults,
	// not a post-filter over the >500 set — the latter could never surface a
	// file below its limit, which is exactly what "what is approaching the wall?"
	// needs to see.
	violations := filesize.CheckViolationsOver(allFiles, b, useBaseline, auditFileSizeOver)

	// The warning band is only meaningful against the real per-type limits; an
	// explicit --over threshold already asks a narrower question.
	var nearLimit []filesize.Violation
	if auditFileSizeOver <= 0 {
		nearLimit = filesize.CheckNearLimit(allFiles)
	}

	// Sort for deterministic output.
	sortViolations(violations)
	sortViolations(nearLimit)

	if auditFileSizeJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		type jsonViolation struct {
			Path     string `json:"path"`
			Lines    int    `json:"lines"`
			Limit    int    `json:"limit"`
			Baseline int    `json:"baseline,omitempty"`
		}
		// The JSON shape stays a bare array of violations — a published CLI
		// contract should not change under callers silently. Machine-readable
		// access to the band is `--over <SoftLimit>`, which reports every file
		// above an arbitrary threshold now that --over is a real threshold.
		out := make([]jsonViolation, len(violations))
		for i, v := range violations {
			out[i] = jsonViolation{v.Path, v.Lines, v.Limit, v.Baseline}
		}
		return enc.Encode(out)
	}

	if len(violations) == 0 {
		baselineNote := ""
		if useBaseline && b != nil && len(b.Files) > 0 {
			baselineNote = fmt.Sprintf(" (%d grandfathered)", len(b.Files))
		}
		fmt.Fprintf(w, "file-size: no violations%s\n", baselineNote)
		renderNearLimit(w, nearLimit)
		return nil
	}

	baselineNote := ""
	if useBaseline && b != nil && len(b.Files) > 0 {
		baselineNote = fmt.Sprintf(" (baseline frozen at %d file(s))", len(b.Files))
	}
	fmt.Fprintf(w, "file-size: %d violation(s)%s\n\n", len(violations), baselineNote)
	fmt.Fprintf(w, "  %-60s  %6s  %6s\n", "FILE", "LINES", "LIMIT")
	fmt.Fprintf(w, "  %-60s  %6s  %6s\n", strings.Repeat("-", 60), "------", "-----")
	for _, v := range violations {
		baselineSuffix := ""
		if v.Baseline > 0 {
			baselineSuffix = fmt.Sprintf(" (baseline %d)", v.Baseline)
		}
		fmt.Fprintf(w, "  %-60s  %6d  %6d%s\n", v.Path, v.Lines, v.Limit, baselineSuffix)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Split oversized files into cohesive modules. Re-run to verify.")
	renderNearLimit(w, nearLimit)
	return nil
}

// renderNearLimit prints the warning band — files that are still compliant but
// close enough that the next edit can push them over. Informational only: it
// never changes the exit code, and it is skipped when there is nothing in band.
func renderNearLimit(w io.Writer, near []filesize.Violation) {
	if len(near) == 0 {
		return
	}
	fmt.Fprintf(w, "\napproaching limit: %d file(s) at >=%d%% of their limit\n\n", len(near), filesize.SoftBandPercent)
	fmt.Fprintf(w, "  %-60s  %6s  %6s\n", "FILE", "LINES", "LIMIT")
	fmt.Fprintf(w, "  %-60s  %6s  %6s\n", strings.Repeat("-", 60), "------", "-----")
	for _, v := range near {
		fmt.Fprintf(w, "  %-60s  %6d  %6d  (%d left)\n", v.Path, v.Lines, v.Limit, v.Limit-v.Lines)
	}
	fmt.Fprintln(w, "\nNot violations — split them on the next touch rather than at the wall.")
}

// sortViolations sorts violations by line count descending, then path.
func sortViolations(vs []filesize.Violation) {
	for i := 1; i < len(vs); i++ {
		for j := i; j > 0 && (vs[j].Lines > vs[j-1].Lines ||
			(vs[j].Lines == vs[j-1].Lines && vs[j].Path < vs[j-1].Path)); j-- {
			vs[j], vs[j-1] = vs[j-1], vs[j]
		}
	}
}
