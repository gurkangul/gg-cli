package cmd

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/graph"
)

var defCmd = &cobra.Command{
	Use:   "def <symbol-name>",
	Short: "Find where a symbol is defined (code graph, offline)",
	Long: `Resolve a symbol name to where it is defined, using the code graph.

This is the grep-free answer to "where is X defined": it returns the defining
file and kind for every Symbol node matching the name (a name can be defined in
more than one file). It reads the embedded graph (.gg/graph.db) — no language
server required — so it is the offline complement to 'gg lsp def', which is the
exact, live oracle when a server is running.

An empty result is reported explicitly, not as a silent "not found": when the
graph is missing or unbuilt the symbol may still exist, so run 'gg index' before
treating an empty result as proof the symbol does not exist.

Requires the code graph (gg index must have been run).`,
	Args: cobra.ExactArgs(1),
	RunE: runDef,
}

func init() {
	rootCmd.AddCommand(defCmd)
}

type defResult struct {
	Query    string              `json:"query"`
	Matches  []graph.SymbolMatch `json:"matches"`
	Warnings []string            `json:"warnings,omitempty"`
}

func runDef(cmd *cobra.Command, args []string) error {
	name, err := requireNonEmpty("symbol-name", args[0])
	if err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	gc, err := graph.New(cfg.DataDir, cfg.ProjectID)
	if err != nil {
		return fmt.Errorf("graph client init: %w", err)
	}
	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()
	defer func() { _ = gc.Close(ctx) }()

	matches, err := gc.FindSymbols(ctx, name)
	if err != nil {
		return err
	}

	result := defResult{Query: name, Matches: matches}
	if len(matches) == 0 {
		result.Warnings = append(result.Warnings,
			"no symbol named "+name+" in the code graph — run 'gg index', or it may be unexported/aliased; an empty result is not proof the symbol does not exist")
	}

	return printJSON(result, func() {
		renderDef(os.Stdout, result)
	})
}

func renderDef(w io.Writer, r defResult) {
	if len(r.Matches) == 0 {
		fmt.Fprintf(w, "No definition of %q found in the code graph.\n", r.Query)
		for _, warn := range r.Warnings {
			fmt.Fprintf(w, "  ~ %s\n", warn)
		}
		return
	}
	fmt.Fprintf(w, "Definitions of %s (%d):\n", r.Query, len(r.Matches))
	for _, m := range r.Matches {
		kind := m.Kind
		if kind == "" {
			kind = "symbol"
		}
		fmt.Fprintf(w, "  %-10s %s\n", kind, m.SourceFile)
	}
}
