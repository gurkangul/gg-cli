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

func Execute() {
	// Cancel the root context on Ctrl+C / SIGTERM so in-flight requests unwind.
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := rootCmd.ExecuteContext(ctx); err != nil {
		// Distinguish cancellation from real errors.
		if errors.Is(err, context.Canceled) {
			os.Exit(130)
		}
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
