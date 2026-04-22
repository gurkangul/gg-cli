package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/config"
)

var systemSyncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Propagate latest gg artifacts (contract + hooks) to every registered project",
	Long: `Iterates ~/.gg/projects.json and runs doctor --fix in each project so
contract updates baked into a new gg binary reach every host-local
project without the user cd'ing to each repo.

Stages per project:
  1. gg doctor --check-contract --fix   (contract block drift repair)
  2. gg doctor --install-agent-hooks    (idempotent agent-hook refresh)
  3. gg doctor --install-task-hooks     (idempotent task-hook refresh)

Projects whose root directory no longer exists are skipped with a
warning — prune them with 'gg system register --prune' after verifying.`,
	RunE: runSystemSync,
}

var (
	systemSyncDryRun       bool
	systemSyncContractOnly bool
)

func init() {
	systemSyncCmd.Flags().BoolVar(&systemSyncDryRun, "dry-run", false,
		"print what would change without writing")
	systemSyncCmd.Flags().BoolVar(&systemSyncContractOnly, "contract-only", false,
		"skip the agent-hook refresh stage (faster when only the contract changed)")
	systemCmd.AddCommand(systemSyncCmd)
}

func runSystemSync(cmd *cobra.Command, _ []string) error {
	reg, err := config.LoadRegistry()
	if err != nil {
		return fmt.Errorf("load registry: %w", err)
	}
	projects := reg.Sorted()
	if len(projects) == 0 {
		fmt.Println("No projects registered.")
		fmt.Println("Run `gg init` in a project to register it, or `gg system register --path /path/to/project`.")
		return nil
	}

	fmt.Printf("Syncing %d project(s)\n", len(projects))
	fmt.Println("──────────────────────────────────────────────────")

	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate gg binary: %w", err)
	}

	var ok, skipped, failed int
	for _, p := range projects {
		fmt.Printf("• %-20s  %s\n", p.Name, p.Root)

		if _, statErr := os.Stat(filepath.Join(p.Root, ".gg", "config.yaml")); statErr != nil {
			fmt.Println("  ✗ skipped — .gg/config.yaml missing (run `gg system register --prune` to clean up)")
			skipped++
			continue
		}

		if systemSyncDryRun {
			fmt.Println("  (dry-run) would run: gg doctor --check-contract --fix")
			if !systemSyncContractOnly {
				fmt.Println("  (dry-run) would run: gg doctor --install-agent-hooks")
				fmt.Println("  (dry-run) would run: gg doctor --install-task-hooks")
			}
			ok++
			continue
		}

		if runErr := runGGIn(self, p.Root, "doctor", "--check-contract", "--fix"); runErr != nil {
			fmt.Printf("  ✗ contract sync failed: %v\n", runErr)
			failed++
			continue
		}
		if !systemSyncContractOnly {
			if runErr := runGGIn(self, p.Root, "doctor", "--install-agent-hooks"); runErr != nil {
				fmt.Printf("  ✗ agent-hook refresh failed: %v\n", runErr)
				failed++
				continue
			}
			if runErr := runGGIn(self, p.Root, "doctor", "--install-task-hooks"); runErr != nil {
				fmt.Printf("  ✗ task-hook refresh failed: %v\n", runErr)
				failed++
				continue
			}
		}
		fmt.Println("  ✓ synced")
		ok++
	}

	fmt.Println("──────────────────────────────────────────────────")
	fmt.Printf("ok=%d skipped=%d failed=%d\n", ok, skipped, failed)
	if failed > 0 {
		return fmt.Errorf("%d project(s) failed to sync", failed)
	}
	return nil
}

// runGGIn executes the gg binary in the given project directory, streaming
// child output through so per-project progress is visible. Child's stderr is
// merged into our stdout so stdin/stdout ordering stays consistent.
func runGGIn(bin, dir string, args ...string) error {
	c := exec.Command(bin, args...)
	c.Dir = dir
	c.Stdout = prefixedWriter{prefix: "    "}
	c.Stderr = prefixedWriter{prefix: "    "}
	return c.Run()
}

// prefixedWriter indents every line from a child command so cross-project
// output stays visually anchored under the project's bullet header.
type prefixedWriter struct{ prefix string }

func (w prefixedWriter) Write(b []byte) (int, error) {
	// Simple line-buffered prefixing; acceptable for doctor's modest output
	// volume. If a project ever produces massive logs here we'd buffer.
	for _, line := range splitKeepNL(string(b)) {
		if line == "\n" {
			_, _ = fmt.Fprint(os.Stdout, line)
			continue
		}
		_, _ = fmt.Fprint(os.Stdout, w.prefix+line)
	}
	return len(b), nil
}

func splitKeepNL(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i+1])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}
