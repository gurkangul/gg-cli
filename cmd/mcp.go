package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/mcp"
)

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Model Context Protocol server — expose the project brain to MCP clients",
	Long: `gg mcp runs a hand-rolled, READ-ONLY Model Context Protocol server.

An MCP client (Claude Desktop, an IDE, another agent) spawns 'gg mcp serve' as a
child process and exchanges newline-delimited JSON-RPC 2.0 messages over the
child's stdin/stdout. There is no port and no daemon.

The server exposes only read tools (gg_search, gg_context, gg_impact, gg_def,
gg_uses, gg_canon, gg_task_get, gg_bug_get). No write tools exist — the brain
cannot be mutated through MCP.`,
}

var mcpServeProject string

var mcpServeCmd = &cobra.Command{
	Use:   "serve",
	Short: "Serve the project brain over MCP (JSON-RPC 2.0 on stdio)",
	Long: `Serve the project brain over a stdio JSON-RPC 2.0 transport.

The project is resolved from the current working directory (walk-up to .gg).
Use --project <path> to point a global-config client at a specific project.

Diagnostics go to stderr; stdout carries only JSON-RPC responses.`,
	Args: cobra.NoArgs,
	RunE: runMCPServe,
}

func init() {
	mcpServeCmd.Flags().StringVar(&mcpServeProject, "project", "", "project directory to serve (overrides CWD-based resolution)")
	mcpCmd.AddCommand(mcpServeCmd)
	rootCmd.AddCommand(mcpCmd)
}

func runMCPServe(cmd *cobra.Command, _ []string) error {
	// --project overrides the CWD-based project resolution: chdir so the
	// internal config.Load() walk-up resolves the requested project's .gg.
	if mcpServeProject != "" {
		if err := os.Chdir(mcpServeProject); err != nil {
			return fmt.Errorf("--project %q: %w", mcpServeProject, err)
		}
	}

	server := mcp.New(mcp.NewHost(), resolveVersion())
	// stdout is the protocol channel — only JSON-RPC responses go there. All
	// diagnostics route to stderr.
	return server.Serve(cmd.Context(), os.Stdin, os.Stdout, os.Stderr)
}
