package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/telemetry"
)

// version is overridable at build time via -ldflags "-X ...cmd.version=vX.Y.Z".
// When left as "dev", resolveVersion() fills in the module version and/or VCS
// revision reported by `go install`, so a user who ran `go install .../cmd/gg@v0.3.0`
// sees "v0.3.0" without any build-time flag wiring.
var version = "dev"

// resolveVersion returns the effective version string. Priority:
//  1. Build-time override (ldflags -X cmd.version=...) — used by release builds.
//  2. Module version from Go's build info (populated by `go install ...@vX.Y.Z`).
//  3. VCS revision + modified flag (populated by `go install` from a git checkout).
//  4. Literal "dev" as a final fallback.
func resolveVersion() string {
	if version != "dev" {
		return version
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}
	if v := info.Main.Version; v != "" && v != "(devel)" {
		return v
	}
	var rev, modified string
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			modified = s.Value
		}
	}
	if rev != "" {
		short := rev
		if len(short) > 7 {
			short = short[:7]
		}
		if modified == "true" {
			return "dev-" + short + "-dirty"
		}
		return "dev-" + short
	}
	return "dev"
}

var rootCmd = &cobra.Command{
	Use:           "gg",
	Short:         "Shared brain for AI agents",
	Long:          "GG — One brain, any agent. A shared knowledge base CLI for AI agents.",
	Version:       resolveVersion(),
	SilenceUsage:  true,
	SilenceErrors: true,
}

func init() {
	rootCmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "output results as JSON")
	rootCmd.PersistentPreRun = func(cmd *cobra.Command, _ []string) {
		// Record telemetry on every command invocation. Compact-aware commands
		// self-record post-render with byte counts — skip here to avoid double
		// counting when --compact is active.
		if f := cmd.Flags().Lookup("compact"); f != nil && f.Changed {
			return
		}
		// Best-effort: silently skip if config can't be loaded.
		if cfg, err := config.Load(); err == nil {
			// Only override the default-ON behaviour when the user explicitly
			// set telemetry.enabled in .gg/config.yaml (see BUG-018). Field
			// absence must not be read as "false".
			if cfg.Telemetry.Enabled != nil {
				telemetry.SetEnabled(*cfg.Telemetry.Enabled)
			}
			if runtimeDir, err := cfg.RuntimeDir(); err == nil {
				// Pass --from flag value if the command defines one — telemetry
				// uses it as a "this is an agent" signal alongside GG_ROLE/GG_AGENT.
				fromFlag := ""
				if f := cmd.Flags().Lookup("from"); f != nil {
					fromFlag = f.Value.String()
				}
				telemetry.Record(runtimeDir, cmd.Name(), fromFlag)
			}
		}
	}
}

// RootCmd returns the root cobra command. Used by tools/docs-gen for
// generating the docs/cli/ reference via cobra/doc.GenMarkdownTree.
func RootCmd() *cobra.Command {
	return rootCmd
}

func Execute() {
	// Cancel the root context on Ctrl+C / SIGTERM so in-flight requests unwind.
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := rootCmd.ExecuteContext(ctx); err != nil {
		if errors.Is(err, context.Canceled) {
			if jsonOutput {
				_ = writeJSON(map[string]any{"error": "interrupted", "code": ExitSignal})
			}
			os.Exit(ExitSignal) //nolint:gocritic // exitAfterDefer: cancel() not running is intentional — signal already cancelled the context
		}

		// Unwrap ExitError to get a structured exit code.
		var exitErr *ExitError
		if errors.As(err, &exitErr) {
			if jsonOutput {
				_ = writeJSON(map[string]any{"error": exitErr.Message, "code": exitErr.Code})
			} else {
				fmt.Fprintln(os.Stderr, "error:", exitErr.Message)
			}
			os.Exit(exitErr.Code)
		}

		if jsonOutput {
			_ = writeJSON(map[string]any{"error": err.Error(), "code": ExitGeneral})
		} else {
			fmt.Fprintln(os.Stderr, "error:", err)
		}
		os.Exit(ExitGeneral)
	}
}
