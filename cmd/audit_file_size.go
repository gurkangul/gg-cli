package cmd

import (
	"encoding/json"
	"fmt"
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

Files in the .gg/file-size-baseline.json grandfather list are only
flagged when they have grown beyond their baseline value.

Use --no-baseline to see raw violations ignoring the grandfather list.
Use --over N to use a custom threshold instead of the per-type defaults.
Use --json for machine-readable output.`,
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

	var b *filesize.Baseline
	useBaseline := !auditFileSizeNoBaseline
	if useBaseline {
		b, err = filesize.ReadBaseline(root)
		if err != nil {
			return fmt.Errorf("reading baseline: %w", err)
		}
	}

	violations := filesize.CheckViolations(allFiles, b, useBaseline)

	// Apply --over filter if provided.
	if auditFileSizeOver > 0 {
		var filtered []filesize.Violation
		for _, v := range violations {
			if v.Lines > auditFileSizeOver {
				filtered = append(filtered, v)
			}
		}
		violations = filtered
	}

	// Sort for deterministic output.
	sortViolations(violations)

	if auditFileSizeJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		type jsonViolation struct {
			Path     string `json:"path"`
			Lines    int    `json:"lines"`
			Limit    int    `json:"limit"`
			Baseline int    `json:"baseline,omitempty"`
		}
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
	return nil
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
