package cmd

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/graph"
)

var usesFile string

var usesCmd = &cobra.Command{
	Use:   "uses <symbol-name>",
	Short: "Find which files use (reference) a symbol — symbol-exact reverse blast-radius",
	Long: `List the files that reference a symbol, resolved from the code graph.

This is the grep-free, barrel-exact answer to "who uses symbol X". Because it
matches REFERENCES edges to the specific Symbol — not its file — a barrel that
re-exports the symbol (export * from './X') never makes a consumer of a sibling
symbol show up here, which is exactly where 2-hop file-level 'gg impact' over-
reports. For the live, type-aware variant use 'gg lsp refs'.

If the name is defined in more than one file, every definition is reported with
its own referencers; use --file to narrow to one.

REFERENCES edges are written only for the semantic (SCIP) tier, so an empty
result on an unbuilt or syntactic-only graph is not proof the symbol is unused —
run 'gg index' first.

Requires the code graph (gg index must have been run).`,
	Args: cobra.ExactArgs(1),
	RunE: runUses,
}

func init() {
	usesCmd.Flags().StringVar(&usesFile, "file", "", "defining source file, to disambiguate a name defined in multiple files")
	rootCmd.AddCommand(usesCmd)
}

type symbolUses struct {
	Symbol      graph.SymbolMatch `json:"symbol"`
	Referencers []string          `json:"referencers"`
}

type usesResult struct {
	Query    string       `json:"query"`
	Uses     []symbolUses `json:"uses"`
	Warnings []string     `json:"warnings,omitempty"`
}

func runUses(cmd *cobra.Command, args []string) error {
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
	if usesFile != "" {
		var filtered []graph.SymbolMatch
		for _, m := range matches {
			if m.SourceFile == usesFile {
				filtered = append(filtered, m)
			}
		}
		matches = filtered
	}

	result := usesResult{Query: name}
	if len(matches) == 0 {
		result.Warnings = append(result.Warnings,
			"no symbol named "+name+" in the code graph — run 'gg index', or it may be unexported/aliased; an empty result is not proof the symbol is unused")
	}
	for _, m := range matches {
		refs, refErr := gc.ReferencersOf(ctx, m.ID)
		if refErr != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("referencers of %s: %v", m.Name, refErr))
			continue
		}
		result.Uses = append(result.Uses, symbolUses{Symbol: m, Referencers: refs})
	}
	if len(result.Uses) > 0 {
		result.Warnings = append(result.Warnings,
			"REFERENCES come from the semantic (SCIP) tier only; dynamic dispatch, reflection, and generated code may be missing")
	}

	return printJSON(result, func() {
		renderUses(os.Stdout, result)
	})
}

func renderUses(w io.Writer, r usesResult) {
	if len(r.Uses) == 0 {
		fmt.Fprintf(w, "No uses of %q found in the code graph.\n", r.Query)
		for _, warn := range r.Warnings {
			fmt.Fprintf(w, "  ~ %s\n", warn)
		}
		return
	}
	for _, u := range r.Uses {
		fmt.Fprintf(w, "Uses of %s (%s) — %d referencing file(s):\n", u.Symbol.Name, u.Symbol.SourceFile, len(u.Referencers))
		if len(u.Referencers) == 0 {
			fmt.Fprintln(w, "  (none indexed)")
		}
		for _, ref := range u.Referencers {
			fmt.Fprintf(w, "  → %s\n", ref)
		}
	}
	for _, warn := range r.Warnings {
		fmt.Fprintf(w, "\n  ~ %s\n", warn)
	}
}
