package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/embedding"
	"github.com/gurkangul/gg-cli/internal/store"
)

var systemSyncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Propagate latest gg artifacts and self-heal tracker collections",
	Long: `Iterates ~/.gg/projects.json and runs tracker self-heal plus doctor --check-contract --fix
in each project so contract, hook, and tracker-collection updates baked into a
new gg binary reach every host-local project without the user cd'ing to each repo.

Stages per project:
  1. tracker self-heal                      (ensure decision/task/message/etc collections)
  2. gg doctor --check-contract --fix      (contract block drift repair)
  3. gg doctor --install-agent-hooks       (idempotent agent-hook refresh)
  4. gg doctor --install-task-hooks        (idempotent task-hook refresh)

Projects whose root directory no longer exists are skipped with a
warning — prune them with 'gg system register --prune' after verifying.
Projects with missing .gg/config.yaml are also skipped, as they were likely
partially removed or migrated away.
`,
	RunE: runSystemSync,
}

type systemSyncQdrant interface {
	HealthCheck(ctx context.Context) error
	CollectionStatus(ctx context.Context) (present, missing []string, err error)
	EnsureCollections(ctx context.Context, vectorSize uint64) error
	Close() error
}

var (
	systemSyncDryRun             bool
	systemSyncContractOnly       bool
	systemSyncContractForceReset bool

	systemSyncRunCommand = runGGIn

	systemSyncNewQdrantClient = func(cfg *config.Config, ggDir string) (systemSyncQdrant, error) {
		return store.New(ggDir, cfg.ProjectID)
	}

	systemSyncTrackerHealthTimeout = 2 * time.Second
	systemSyncTrackerStatusTimeout = 2 * time.Second
)

func init() {
	systemSyncCmd.Flags().BoolVar(&systemSyncDryRun, "dry-run", false,
		"print what would change without writing")
	systemSyncCmd.Flags().BoolVar(&systemSyncContractOnly, "contract-only", false,
		"skip the agent-hook refresh stage (faster when only the contract changed)")
	systemSyncCmd.Flags().BoolVar(&systemSyncContractForceReset, "contract-force-reset", false,
		"pass --force-reset to gg doctor --check-contract --fix (overwrites manually-edited contract blocks)")
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

	stats := registryStats(reg)
	fmt.Printf("Registry: %d project%s (%d ok, %d stale, %d invalid)\n",
		stats.Total, plural(stats.Total, "", "s"), stats.OK, stats.Missing, stats.Invalid)
	if stats.Missing > 0 {
		fmt.Println("Run `gg system register --prune` to remove stale entries (missing root directory).")
	}

	fmt.Printf("Syncing %d project(s)\n", len(projects))
	fmt.Println("──────────────────────────────────────────────────")

	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate gg binary: %w", err)
	}

	var ok, skipped, failed int
	for _, p := range projects {
		if err := cmd.Context().Err(); err != nil {
			return err
		}
		fmt.Printf("• %-20s  %s\n", p.Name, p.Root)

		if _, statErr := os.Stat(p.Root); statErr != nil {
			fmt.Println("  ✗ skipped — project root missing (run `gg system register --prune` to clean up)")
			skipped++
			continue
		}

		configPath := filepath.Join(p.Root, ".gg", "config.yaml")
		if _, statErr := os.Stat(configPath); statErr != nil {
			fmt.Println("  ✗ skipped — .gg/config.yaml missing (run `gg system register --prune` to clean up)")
			skipped++
			continue
		}

		cfg, cfgErr := config.LoadFromGGDir(filepath.Join(p.Root, config.DirName))
		if cfgErr != nil {
			fmt.Printf("  ✗ invalid config at %s: %v\n", configPath, cfgErr)
			failed++
			continue
		}

		if !systemSyncDryRun {
			if err := runSystemSyncTracker(cmd.Context(), p.Root, cfg); err != nil {
				if ctxErr := cmd.Context().Err(); ctxErr != nil {
					return ctxErr
				}
				if errors.Is(err, context.Canceled) {
					return err
				}
				fmt.Printf("  ✗ tracker self-heal failed: %v\n", err)
				failed++
				continue
			}
			if err := cmd.Context().Err(); err != nil {
				return err
			}
		} else {
			runSystemSyncDryRunStages(p.Root)
			ok++
			continue
		}

		contractArgs := []string{"doctor", "--check-contract", "--fix"}
		if systemSyncContractForceReset {
			contractArgs = append(contractArgs, "--force-reset")
		}
		if runErr := systemSyncRunCommand(self, p.Root, contractArgs...); runErr != nil {
			fmt.Printf("  ✗ contract sync failed: %v\n", runErr)
			failed++
			continue
		}
		if !systemSyncContractOnly {
			if runErr := systemSyncRunCommand(self, p.Root, "doctor", "--install-agent-hooks"); runErr != nil {
				fmt.Printf("  ✗ agent-hook refresh failed: %v\n", runErr)
				failed++
				continue
			}
			if runErr := systemSyncRunCommand(self, p.Root, "doctor", "--install-task-hooks"); runErr != nil {
				fmt.Printf("  ✗ task-hook refresh failed: %v\n", runErr)
				failed++
				continue
			}
			if drifted, err := reportHookTemplateDriftForProject(p.Root); err != nil {
				fmt.Printf("  ✗ hook template drift check failed: %v\n", err)
				failed++
				continue
			} else if drifted > 0 {
				fmt.Println("  Run gg doctor --refresh-hooks to apply pending template updates.")
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

func runSystemSyncTracker(ctx context.Context, projectRoot string, cfg *config.Config) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	ggDir := filepath.Join(projectRoot, config.DirName)
	vectorSize, err := systemSyncVectorSize(ggDir)
	if err != nil {
		return err
	}
	client, err := systemSyncNewQdrantClient(cfg, ggDir)
	if err != nil {
		return fmt.Errorf("create qdrant client: %w", err)
	}
	defer func() { _ = client.Close() }()

	healthCtx, cancel := context.WithTimeout(ctx, systemSyncTrackerHealthTimeout)
	err = client.HealthCheck(healthCtx)
	cancel()
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if errors.Is(err, context.Canceled) {
			return err
		}
		fmt.Printf("  ↷ tracker self-heal skipped — qdrant unavailable: %v\n", err)
		return nil
	}

	statusCtx, cancel := context.WithTimeout(ctx, systemSyncTrackerStatusTimeout)
	present, missing, err := client.CollectionStatus(statusCtx)
	cancel()
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if systemSyncQdrantUnavailable(err) {
			fmt.Printf("  ↷ tracker self-heal skipped — qdrant unavailable: %v\n", err)
			return nil
		}
		return fmt.Errorf("check tracker collections: %w", err)
	}
	if len(missing) == 0 {
		fmt.Printf("  ✓ tracker collections present (%d)\n", len(present))
		return nil
	}

	statusCtx, cancel = context.WithTimeout(ctx, systemSyncTrackerStatusTimeout)
	err = client.EnsureCollections(statusCtx, vectorSize)
	cancel()
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if systemSyncQdrantUnavailable(err) {
			fmt.Printf("  ↷ tracker self-heal skipped — qdrant unavailable: %v\n", err)
			return nil
		}
		if systemSyncAlreadyExists(err) {
			return runSystemSyncRetryAfterRace(ctx, client, vectorSize)
		}
		return fmt.Errorf("ensure tracker collections: %w", err)
	}
	fmt.Printf("  ✓ tracker collections ensured: %d missing -> created\n", len(missing))
	return nil
}

func systemSyncVectorSize(ggDir string) (uint64, error) {
	meta, err := embedding.ReadMeta(ggDir)
	if err != nil {
		return 0, err
	}
	if meta == nil || meta.Dim <= 0 {
		return uint64(store.VectorSize), nil
	}
	return uint64(meta.Dim), nil
}

func runSystemSyncRetryAfterRace(ctx context.Context, client systemSyncQdrant, vectorSize uint64) error {
	statusCtx, cancel := context.WithTimeout(ctx, systemSyncTrackerStatusTimeout)
	_, missing, err := client.CollectionStatus(statusCtx)
	cancel()
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if systemSyncQdrantUnavailable(err) {
			fmt.Printf("  ↷ tracker self-heal skipped — qdrant unavailable: %v\n", err)
			return nil
		}
		return fmt.Errorf("verify tracker collections after create race: %w", err)
	}
	if len(missing) == 0 {
		fmt.Println("  ✓ tracker collections ensured by concurrent sync")
		return nil
	}

	statusCtx, cancel = context.WithTimeout(ctx, systemSyncTrackerStatusTimeout)
	err = client.EnsureCollections(statusCtx, vectorSize)
	cancel()
	if err == nil {
		fmt.Printf("  ✓ tracker collections ensured after create race: %d missing -> created\n", len(missing))
		return nil
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	if systemSyncQdrantUnavailable(err) {
		fmt.Printf("  ↷ tracker self-heal skipped — qdrant unavailable: %v\n", err)
		return nil
	}
	if systemSyncAlreadyExists(err) {
		return runSystemSyncFinalRaceCheck(ctx, client)
	}
	return fmt.Errorf("ensure tracker collections after create race: %w", err)
}

func runSystemSyncFinalRaceCheck(ctx context.Context, client systemSyncQdrant) error {
	statusCtx, cancel := context.WithTimeout(ctx, systemSyncTrackerStatusTimeout)
	_, missing, err := client.CollectionStatus(statusCtx)
	cancel()
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if systemSyncQdrantUnavailable(err) {
			fmt.Printf("  ↷ tracker self-heal skipped — qdrant unavailable: %v\n", err)
			return nil
		}
		return fmt.Errorf("verify tracker collections after repeated create race: %w", err)
	}
	if len(missing) == 0 {
		fmt.Println("  ✓ tracker collections ensured by concurrent sync")
		return nil
	}
	return fmt.Errorf("ensure tracker collections raced; still missing %d collection(s)", len(missing))
}

func systemSyncQdrantUnavailable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, store.ErrQdrantDown) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "deadline exceeded") ||
		strings.Contains(msg, "unavailable")
}

func systemSyncAlreadyExists(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "already exists")
}

func runSystemSyncDryRunStages(root string) {
	fmt.Println("  (dry-run) would check tracker collections (collection status + ensure)")

	contractCmd := "gg doctor --check-contract --fix"
	if systemSyncContractForceReset {
		contractCmd += " --force-reset"
	}
	fmt.Printf("  (dry-run) would run: %s\n", contractCmd)

	if !systemSyncContractOnly {
		fmt.Println("  (dry-run) would run: gg doctor --install-agent-hooks")
		fmt.Println("  (dry-run) would run: gg doctor --install-task-hooks")
		if drifted, err := countHookTemplateDriftInProject(root); err == nil {
			fmt.Printf("  (dry-run) would refresh %d drifted hook(s)\n", drifted)
		} else {
			fmt.Printf("  (dry-run) hook template drift check skipped: %v\n", err)
		}
	}
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

func countHookTemplateDriftInProject(root string) (int, error) {
	var drifted int
	err := withWorkingDir(root, func() error {
		checks, err := checkHookTemplates()
		if err != nil {
			return err
		}
		for _, check := range checks {
			if check.state == hookTemplateDrift {
				drifted++
			}
		}
		return nil
	})
	return drifted, err
}

func reportHookTemplateDriftForProject(root string) (int, error) {
	var drifted int
	err := withWorkingDir(root, func() error {
		checks, err := checkHookTemplates()
		if err != nil {
			return err
		}
		fmt.Println("  Hook templates:")
		if len(checks) == 0 {
			fmt.Println("    ✓ no deployed hook templates found")
			return nil
		}
		for _, check := range checks {
			rel, _ := filepath.Rel(root, check.spec.path)
			label := filepath.ToSlash(rel)
			switch check.state {
			case hookTemplateInSync:
				fmt.Printf("    ✓ %-42s in-sync\n", label)
			case hookTemplateDrift:
				drifted++
				fmt.Printf("    ⚠ %-42s drift (%s)\n", label, hookTemplateDriftDetail(check))
			case hookTemplateUserCustomized:
				fmt.Printf("    ~ %-42s user-customized\n", label)
			}
		}
		return nil
	})
	return drifted, err
}

func withWorkingDir(dir string, fn func() error) error {
	previous, err := os.Getwd()
	if err != nil {
		return err
	}
	if err := os.Chdir(dir); err != nil {
		return err
	}
	defer func() { _ = os.Chdir(previous) }()
	return fn()
}
