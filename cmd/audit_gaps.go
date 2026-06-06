package cmd

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/config"
)

// gapsDefaultSkipPrefixes are path prefixes excluded from coverage reporting
// by default because they contain auto-generated or test-only files that
// cannot meaningfully be referenced in gg tasks/decisions.
var gapsDefaultSkipPrefixes = []string{
	"docs/cli/",     // auto-generated CLI reference docs
	"cmd/testdata/", // command golden fixtures
	"testdata/",     // regression test fixtures
	"_bmad",         // local planning artifacts
	"examples/",     // packaged demo data
	".gsd/",         // local planning workspace
	".claude/",      // Claude config artifacts
}

var gapsDefaultSkipSuffixes = []string{
	".golden",
	".expected.json",
	".test",
	".out",
}

var auditGapsCmd = &cobra.Command{
	Use:   "gaps",
	Short: "List files with recent git commits but no gg record/decision/task coverage",
	Long: `Walk git log for the look-back window (default 7d) and report files
that were committed but never referenced in any gg task, decision, or record.

Use this as a weekly retrospective companion to gg audit report (which fires
live at session end). Helps maintainers spot knowledge-capture gaps without
reading every commit.`,
	Args: cobra.NoArgs,
	RunE: runAuditGaps,
}

// parseSinceFlag converts a duration string like "7d" or "14d" to a --since
// value accepted by git log (e.g. "7 days ago").
func parseSinceFlag(s string) (string, error) {
	s = strings.TrimSpace(s)
	if strings.HasSuffix(s, "d") {
		days := strings.TrimSuffix(s, "d")
		days = strings.TrimSpace(days)
		if days == "" || strings.ContainsAny(days, " \t") {
			return "", fmt.Errorf("invalid --since value %q: expected NNd (e.g. 7d)", s)
		}
		return days + " days ago", nil
	}
	return "", fmt.Errorf("invalid --since value %q: only NNd format supported (e.g. 7d, 14d)", s)
}

// gitChangedFiles returns the set of files changed in commits since the given
// git --since value (e.g. "7 days ago").
func gitChangedFiles(projectRoot, since string) ([]string, error) {
	// --diff-filter=d excludes deleted files — they can't be in the current codebase.
	out, err := exec.Command(
		"git", "-C", projectRoot,
		"log", "--since="+since, "--name-only", "--diff-filter=d", "--pretty=format:",
	).Output()
	if err != nil {
		return nil, fmt.Errorf("git log: %w", err)
	}

	seen := map[string]bool{}
	var files []string
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || seen[line] {
			continue
		}
		seen[line] = true
		files = append(files, line)
	}
	return files, scanner.Err()
}

// buildGGCorpus returns a slice of all text blobs from gg tasks, decisions,
// and rejections. Each blob is the concatenated searchable text for one entry.
func buildGGCorpus(d *deps) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var corpus []string

	tasks, err := d.store.ListTasks(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	for _, t := range tasks {
		corpus = append(corpus, strings.ToLower(t.Title+" "+t.Detail))
	}

	decisions, err := d.store.ListDecisions(ctx, 0, true)
	if err != nil {
		return nil, fmt.Errorf("list decisions: %w", err)
	}
	for _, dec := range decisions {
		corpus = append(corpus, strings.ToLower(dec.Text+" "+dec.Reason))
	}

	rejections, err := d.store.ListRejections(ctx, 0)
	if err != nil {
		return nil, fmt.Errorf("list rejections: %w", err)
	}
	for _, r := range rejections {
		corpus = append(corpus, strings.ToLower(r.Approach+" "+r.Reason))
	}

	return corpus, nil
}

// fileIsCovered returns true if any corpus blob mentions the file path or its
// base name.
func fileIsCovered(file string, corpus []string) bool {
	lowerFile := strings.ToLower(file)
	lowerBase := strings.ToLower(filepath.Base(file))
	for _, blob := range corpus {
		if strings.Contains(blob, lowerFile) || strings.Contains(blob, lowerBase) {
			return true
		}
	}
	return false
}

// filterGapsFiles removes generated, fixture, binary, and coverage artifacts
// that make the audit noisy but do not represent missing project rationale.
func filterGapsFiles(files, skipPrefixes, skipSuffixes []string) []string {
	out := files[:0:len(files)]
	for _, f := range files {
		if !gapsFileIsNoise(f, skipPrefixes, skipSuffixes) {
			out = append(out, f)
		}
	}
	return out
}

func gapsFileIsNoise(file string, skipPrefixes, skipSuffixes []string) bool {
	for _, p := range skipPrefixes {
		if strings.HasPrefix(file, p) {
			return true
		}
	}
	for _, s := range skipSuffixes {
		if strings.HasSuffix(file, s) {
			return true
		}
	}
	return false
}

func runAuditGaps(cmd *cobra.Command, _ []string) error {
	since, err := parseSinceFlag(auditGapsSince)
	if err != nil {
		return err
	}

	projRoot, err := config.FindRoot()
	if err != nil {
		return fmt.Errorf("find project root: %w", err)
	}

	files, err := gitChangedFiles(projRoot, since)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "No commits found in the last %s.\n", auditGapsSince)
		return nil
	}

	if !auditGapsIncludeAll {
		files = filterGapsFiles(files, gapsDefaultSkipPrefixes, gapsDefaultSkipSuffixes)
	}

	d, err := loadDepsReadOnly(false)
	if err != nil {
		return err
	}
	defer d.Close()

	if d.qdrantDown {
		fmt.Fprintln(cmd.ErrOrStderr(), "warning: Qdrant unreachable — coverage check skipped, listing all changed files as gaps")
		for _, f := range files {
			fmt.Fprintln(cmd.OutOrStdout(), f)
		}
		return nil
	}

	corpus, err := buildGGCorpus(d)
	if err != nil {
		return err
	}

	var gaps []string
	for _, f := range files {
		if !fileIsCovered(f, corpus) {
			gaps = append(gaps, f)
		}
	}

	w := cmd.OutOrStdout()
	if len(gaps) == 0 {
		fmt.Fprintf(w, "No gaps — all %d changed files have gg coverage.\n", len(files))
		return nil
	}

	if isCompactActive(cmd) {
		emitCompact(cmd, "gaps",
			func(out io.Writer) { renderAuditGapsDefault(out, gaps, len(files), auditGapsSince) },
			func(out io.Writer) { renderAuditGapsCompact(out, gaps) },
			compactRendererV_auditGaps,
		)
		return nil
	}

	renderAuditGapsDefault(w, gaps, len(files), auditGapsSince)
	return nil
}

func renderAuditGapsCompact(w io.Writer, gaps []string) {
	for _, f := range gaps {
		fmt.Fprintln(w, f)
	}
}

func renderAuditGapsDefault(w io.Writer, gaps []string, totalFiles int, since string) {
	fmt.Fprintf(w, "gaps: %d of %d changed files have no gg record/decision/task coverage (last %s)\n\n",
		len(gaps), totalFiles, since)
	for _, f := range gaps {
		fmt.Fprintf(w, "  • %s\n", f)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Run `gg record` after editing files to capture rationale.")
}
