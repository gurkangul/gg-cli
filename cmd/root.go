package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
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
			os.Exit(ExitSignal)
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
