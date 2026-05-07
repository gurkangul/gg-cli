package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/config"
)

var bugRunReprosCmd = &cobra.Command{
	Use:   "run-repros",
	Short: "Run all registered repro scripts for fixed bugs",
	Long: `Runs every repro script attached to a fixed bug.
Exits 0 if all pass, exits 7 if any fail (regression detected).
Used by .gg/hooks/pre-task-done.d/90-bug-repros.sh.`,
	Args: cobra.NoArgs,
	RunE: runBugRunRepros,
}

var bugRunReprosBudget int

func init() {
	bugRunReprosCmd.Flags().IntVar(&bugRunReprosBudget, "budget", 120, "per-repro timeout in seconds")
	bugCmd.AddCommand(bugRunReprosCmd)
}

type reproResult struct {
	bugID  string
	path   string
	passed bool
	output string
	dur    time.Duration
}

func runBugRunRepros(cmd *cobra.Command, _ []string) error {
	d, err := loadDeps(false)
	if err != nil {
		return err
	}
	defer d.Close()

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	// Resolve project root so relative repro paths work from any CWD.
	ggDir, err := config.GGDir()
	if err != nil {
		return fmt.Errorf("find .gg dir: %w", err)
	}
	projectRoot := filepath.Dir(ggDir)

	bugs, err := d.store.ListBugs(ctx, "fixed")
	if err != nil {
		return fmt.Errorf("list fixed bugs: %w", err)
	}

	type pending struct {
		bugID string
		path  string
	}
	var toRun []pending
	for _, b := range bugs {
		if b.ReproScript == "" {
			continue
		}
		p := b.ReproScript
		if !filepath.IsAbs(p) {
			p = filepath.Join(projectRoot, p)
		}
		toRun = append(toRun, pending{bugID: b.ID, path: p})
	}

	if len(toRun) == 0 {
		fmt.Println("ℹ no repro scripts registered — skipping regression gate")
		return nil
	}

	fmt.Printf("Running %d repro script(s)...\n", len(toRun))

	perReproBudget := time.Duration(bugRunReprosBudget) * time.Second
	var results []reproResult
	for _, p := range toRun {
		rctx, rcancel := context.WithTimeout(cmd.Context(), perReproBudget)
		result := runSingleRepro(rctx, p.bugID, p.path)
		rcancel()
		if !result.passed && rctx.Err() == context.DeadlineExceeded {
			result.output = fmt.Sprintf("repro exceeded per-repro budget (%s)", perReproBudget)
		}
		results = append(results, result)
	}

	var failed []reproResult
	for _, r := range results {
		icon := "✓"
		if !r.passed {
			icon = "✗"
			failed = append(failed, r)
		}
		fmt.Printf("  %s %s  %s  (%s)\n", icon, r.bugID, r.path, r.dur.Round(time.Millisecond))
	}

	if len(failed) == 0 {
		fmt.Printf("✓ all %d repros passed\n", len(results))
		return nil
	}

	fmt.Printf("\n✗ %d repro(s) FAILED — regression detected:\n\n", len(failed))
	for _, r := range failed {
		fmt.Printf("--- %s (%s) ---\n%s\n\n", r.bugID, r.path, r.output)
	}
	return &ExitError{Code: ExitVerifyFailed, Message: fmt.Sprintf("%d repro(s) failed — regression detected", len(failed))}
}

func runSingleRepro(ctx context.Context, bugID, path string) reproResult {
	start := time.Now()
	//nolint:gosec // path is validated at attach time
	c := exec.CommandContext(ctx, "sh", path)
	c.Env = scrubReproEnv(os.Environ())
	out, err := c.CombinedOutput()
	dur := time.Since(start)
	passed := err == nil
	tail := string(out)
	if lines := strings.Split(strings.TrimRight(tail, "\n"), "\n"); len(lines) > 10 {
		tail = strings.Join(lines[len(lines)-10:], "\n")
	}
	return reproResult{bugID: bugID, path: path, passed: passed, output: tail, dur: dur}
}

func scrubReproEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		if strings.HasPrefix(kv, "GG_ALLOW_INBOX_SKIP=") {
			continue
		}
		out = append(out, kv)
	}
	return out
}
