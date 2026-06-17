package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/lsp"
)

// lspTimeout bounds the entire spawn→init→query→shutdown exchange so a hung
// language server can never wedge the command.
const lspTimeout = 15 * time.Second

var lspCmd = &cobra.Command{
	Use:   "lsp",
	Short: "Live, type-aware code intelligence via a language server",
	Long: `Query a running language server for EXACT, type-aware, never-stale code
intelligence — references, definitions, and hover — for a symbol at a precise
file position. Unlike gg's indexed code graph (only as fresh as the last
index), lsp answers from a language server launched for this one invocation.

  gg lsp refs  <file> <line> <col>   callers/usages of the symbol
  gg lsp defn  <file> <line> <col>   the symbol's definition location(s)
  gg lsp hover <file> <line> <col>   the symbol's signature / documentation

line and col are 1-based (editor convention). The language server is resolved
by file extension (.go → gopls); per-invocation only — no daemon.`,
}

var lspRefsCmd = &cobra.Command{
	Use:   "refs <file> <line> <col>",
	Short: "Find references (callers/usages) of the symbol at a position",
	Args:  cobra.ExactArgs(3),
	RunE:  func(cmd *cobra.Command, args []string) error { return runLSP(cmd, lsp.KindReferences, args) },
}

var lspDefnCmd = &cobra.Command{
	Use:   "defn <file> <line> <col>",
	Short: "Jump to the definition of the symbol at a position",
	Args:  cobra.ExactArgs(3),
	RunE:  func(cmd *cobra.Command, args []string) error { return runLSP(cmd, lsp.KindDefinition, args) },
}

var lspHoverCmd = &cobra.Command{
	Use:   "hover <file> <line> <col>",
	Short: "Show the signature/documentation of the symbol at a position",
	Args:  cobra.ExactArgs(3),
	RunE:  func(cmd *cobra.Command, args []string) error { return runLSP(cmd, lsp.KindHover, args) },
}

func init() {
	lspCmd.AddCommand(lspRefsCmd, lspDefnCmd, lspHoverCmd)
	rootCmd.AddCommand(lspCmd)
}

func runLSP(cmd *cobra.Command, kind lsp.Kind, args []string) error {
	file := args[0]
	line, err := parsePositive(args[1], "line")
	if err != nil {
		return err
	}
	col, err := parsePositive(args[2], "col")
	if err != nil {
		return err
	}
	if _, statErr := os.Stat(file); statErr != nil {
		return notFound(fmt.Sprintf("file not found: %s", file))
	}

	// Workspace root: the gg project root when inside one, else cwd. gopls wants
	// the module/workspace root to resolve cross-file references correctly.
	rootDir := ""
	if root, rErr := config.FindRoot(); rErr == nil {
		rootDir = root
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), lspTimeout)
	defer cancel()

	res, err := lsp.Query(ctx, kind, file, line, col, rootDir)
	if err != nil {
		return err
	}

	switch kind {
	case lsp.KindHover:
		return printLSPHover(res)
	default:
		return printLSPLocations(kind, res)
	}
}

// parsePositive parses a 1-based line/col argument, rejecting non-numeric and
// non-positive values with a clear message.
func parsePositive(raw, name string) (int, error) {
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s must be a number, got %q", name, raw)
	}
	if n < 1 {
		return 0, fmt.Errorf("%s is 1-based and must be >= 1, got %d", name, n)
	}
	return n, nil
}

// lspLocationJSON is the structured per-location shape (1-based for humans/tools).
type lspLocationJSON struct {
	File    string `json:"file"`
	Line    int    `json:"line"`
	Col     int    `json:"col"`
	EndLine int    `json:"end_line"`
	EndCol  int    `json:"end_col"`
}

func printLSPLocations(kind lsp.Kind, res lsp.Result) error {
	locs := toLSPLocationJSON(res)
	return printJSON(map[string]any{"kind": string(kind), "locations": locs}, func() {
		if len(locs) == 0 {
			fmt.Printf("No %s found.\n", kind)
			return
		}
		for _, l := range locs {
			fmt.Printf("%s:%d:%d\n", l.File, l.Line, l.Col)
		}
	})
}

// toLSPLocationJSON converts LSP 0-based UTF-16 ranges into 1-based rune
// positions for display. The start position is mapped against the queried
// file's text; cross-file ranges fall back to (range+1) since we don't open
// every target — start line/col are accurate, end is best-effort.
func toLSPLocationJSON(res lsp.Result) []lspLocationJSON {
	out := make([]lspLocationJSON, 0, len(res.Locations))
	for _, loc := range res.Locations {
		path := lsp.URIToPath(loc.URI)
		startLine, startCol := loc.Range.Start.Line+1, loc.Range.Start.Character+1
		endLine, endCol := loc.Range.End.Line+1, loc.Range.End.Character+1
		out = append(out, lspLocationJSON{
			File:    path,
			Line:    startLine,
			Col:     startCol,
			EndLine: endLine,
			EndCol:  endCol,
		})
	}
	return out
}

func printLSPHover(res lsp.Result) error {
	text := res.Hover.PlainText
	return printJSON(map[string]any{"kind": "hover", "hover": text}, func() {
		if text == "" {
			fmt.Println("No hover information available.")
			return
		}
		writeLines(os.Stdout, text)
	})
}

// writeLines prints s with a trailing newline guaranteed.
func writeLines(w io.Writer, s string) {
	fmt.Fprint(w, s)
	if len(s) == 0 || s[len(s)-1] != '\n' {
		fmt.Fprintln(w)
	}
}
