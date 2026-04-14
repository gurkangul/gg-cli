package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/gurkangul/gg/internal/config"
	"github.com/gurkangul/gg/internal/index/runner"
	"github.com/spf13/cobra"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Diagnose and repair gg configuration",
	Long: `Check service connectivity and verify that required indexer binaries
are available. Use --install-indexers to automatically install missing SCIP
binaries using their native package managers (go install, npm, pip).`,
	RunE: runDoctor,
}

var doctorInstallIndexers bool

func init() {
	doctorCmd.Flags().BoolVar(&doctorInstallIndexers, "install-indexers", false,
		"install missing SCIP indexer binaries (scip-go, scip-typescript, scip-python)")
	rootCmd.AddCommand(doctorCmd)
}

// indexerSpec describes how to install a SCIP binary.
type indexerSpec struct {
	Binary  string   // name passed to runner.ResolveIndexer
	Install []string // command + args for native package manager
	Note    string   // displayed after a failed install
}

// indexers lists the SCIP binaries gg needs, with install commands.
// We use native package managers rather than raw binary downloads — simpler,
// more reliable, and automatically verifies checksums via the package manager.
// Docker fallback is day-2.
var indexers = []indexerSpec{
	{
		Binary:  "scip-go",
		Install: []string{"go", "install", "github.com/sourcegraph/scip-go/cmd/scip-go@latest"},
		Note:    "requires Go 1.21+",
	},
	{
		Binary:  "scip-typescript",
		Install: []string{"npm", "install", "-g", "@sourcegraph/scip-typescript"},
		Note:    "requires Node.js 18+",
	},
	{
		Binary:  "scip-python",
		Install: []string{"pip", "install", "scip-python"},
		Note:    "requires Python 3.9+",
	},
}

func runDoctor(cmd *cobra.Command, _ []string) error {
	fmt.Println("GG Doctor")
	fmt.Println(strings.Repeat("─", 40))

	checkIndexers(cmd)
	checkServices(cmd)

	return nil
}

// checkIndexers verifies each SCIP binary. With --install-indexers, missing
// ones are installed via their native package manager.
func checkIndexers(cmd *cobra.Command) {
	fmt.Println("\nIndexer Binaries:")
	for _, spec := range indexers {
		path, err := runner.ResolveIndexer(spec.Binary)
		if err == nil {
			fmt.Printf("  ✓ %-22s %s\n", spec.Binary, path)
			continue
		}

		var missing *runner.ErrIndexerMissing
		if !errors.As(err, &missing) {
			fmt.Printf("  ? %-22s unexpected error: %v\n", spec.Binary, err)
			continue
		}

		if !doctorInstallIndexers {
			fmt.Printf("  ✗ %-22s not found\n", spec.Binary)
			fmt.Printf("    install: %s\n", missing.Hint)
			fmt.Printf("    or run:  gg doctor --install-indexers\n")
			continue
		}

		fmt.Printf("  ↓ %-22s installing via %s ...\n", spec.Binary, spec.Install[0])
		if installErr := runInstall(cmd, spec); installErr != nil {
			fmt.Printf("    ✗ failed: %v\n", installErr)
			if spec.Note != "" {
				fmt.Printf("    note: %s\n", spec.Note)
			}
		} else {
			if finalPath, resolveErr := runner.ResolveIndexer(spec.Binary); resolveErr == nil {
				fmt.Printf("    ✓ installed at %s\n", finalPath)
			} else {
				// Binary went to a directory not yet on PATH (e.g. GOPATH/bin).
				fmt.Printf("    ✓ installed (ensure install dir is on PATH)\n")
				if spec.Note != "" {
					fmt.Printf("    note: %s\n", spec.Note)
				}
			}
		}
	}
}

// runInstall executes the install command for the given spec. Stdout/stderr
// are forwarded to the terminal so the user sees live progress.
func runInstall(cmd *cobra.Command, spec indexerSpec) error {
	args := spec.Install
	if runtime.GOOS == "windows" {
		// On Windows, npm and pip are .cmd scripts — wrap in cmd.exe.
		args = append([]string{"cmd", "/C"}, args...)
	}
	c := exec.CommandContext(cmd.Context(), args[0], args[1:]...)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		return fmt.Errorf("%s: %w", strings.Join(spec.Install, " "), err)
	}
	return nil
}

// checkServices probes Qdrant and Ollama connectivity. Failures are warnings —
// doctor itself always exits 0 (the user may run doctor before init).
func checkServices(cmd *cobra.Command) {
	fmt.Println("\nService Connectivity:")

	cfg, cfgErr := config.Load()
	if cfgErr != nil {
		fmt.Printf("  ? config: could not load — %v\n", cfgErr)
		fmt.Println("    run 'gg init' to create a project config")
		return
	}

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	// Qdrant
	d, err := loadDeps(false)
	if err != nil {
		fmt.Printf("  ✗ qdrant:   %v\n", err)
	} else {
		defer d.Close()
		if err := d.store.HealthCheck(ctx); err != nil {
			fmt.Printf("  ✗ qdrant:   %v\n", err)
		} else {
			fmt.Println("  ✓ qdrant:   ok")
		}
	}

	// Ollama — lightweight check: HEAD the API root.
	ollamaURL := cfg.Embedding.Host + "/api/tags"
	curlCtx, curlCancel := withTimeout(cmd.Context())
	defer curlCancel()
	c := exec.CommandContext(curlCtx, "curl", "-sf", "--max-time", "3", ollamaURL)
	c.Stdout = nil
	c.Stderr = nil
	if c.Run() == nil {
		fmt.Printf("  ✓ ollama:   ok (%s)\n", cfg.Embedding.Host)
	} else {
		fmt.Printf("  ✗ ollama:   unreachable at %s\n", cfg.Embedding.Host)
	}
}
