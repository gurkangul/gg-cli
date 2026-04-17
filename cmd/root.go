package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/telemetry"
)

// version is overridable at build time via -ldflags "-X ...cmd.version=vX.Y.Z".
var version = "dev"

var rootCmd = &cobra.Command{
	Use:           "gg",
	Short:         "Shared brain for AI agents",
	Long:          "GG — One brain, any agent. A shared knowledge base CLI for AI agents.",
	Version:       version,
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
			telemetry.SetEnabled(cfg.Telemetry.Enabled)
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
