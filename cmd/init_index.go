package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// maybeRunIndex decides whether to invoke `gg index` after init, based on
// flags, TTY detection, and graph-store availability. Returns true if an index
// run actually completed successfully so the bootstrap prompt can omit the
// "Next: run gg index" suggestion.
//
// graphReady is true when the graph store is usable for indexing: always so for
// the embedded SQLite backend (the default), or when the Memgraph server came
// up for the opt-in server backend.
//
// Decision precedence:
//   - --no-index or !graphReady: skip silently (graph store unavailable)
//   - --with-index: run without prompting
//   - TTY stdin: prompt "Run `gg index --lang X` now? [Y/n]"
//   - non-TTY default: skip (matches the 'interactive opt-in' project rule)
func maybeRunIndex(cmd *cobra.Command, langHint string, graphReady bool) bool {
	if initNoIndex || !graphReady {
		return false
	}
	// langHint == "" means no supported language detected (BUG-033 fix).
	// Previously this fell through with langHint="go" and prompted users
	// to run `gg index --lang go` on .NET/Java projects, which always failed.
	if langHint == "" {
		return false
	}
	if initWithIndex && initNoIndex {
		// Mutually exclusive but --no-index already returned above; this is
		// defensive: prefer the more conservative choice if both are passed.
		return false
	}
	shouldRun := initWithIndex
	if langHint == "swift" {
		fmt.Fprintln(cmd.OutOrStdout(), "Note: Swift CodeGraph requires a compatible external scip-swift binary on PATH or ~/.gg/bin; gg doctor --install-indexers will not install one.")
	}
	if !shouldRun {
		if !isTerminal(os.Stdin) {
			return false
		}
		fmt.Println()
		fmt.Printf("Run `gg index --lang %s` now? This builds the code graph (5-10 min). [Y/n]: ", langHint)
		line, err := newStdinReader().ReadString('\n')
		// EOF with no content = closed stdin (e.g. /dev/null, test harness)
		// — treat as decline, NOT as "user pressed Enter" (which defaults Y).
		if errors.Is(err, io.EOF) && line == "" {
			return false
		}
		answer := strings.ToLower(strings.TrimSpace(line))
		switch answer {
		case "", "y", "yes":
			shouldRun = true
		default:
			shouldRun = false
		}
	}
	if !shouldRun {
		return false
	}
	fmt.Println()
	fmt.Printf("Running `gg index --lang %s`...\n", langHint)
	indexLang = langHint
	indexChanged = false
	if err := runIndex(cmd, nil); err != nil {
		fmt.Fprintf(os.Stderr, "⚠ gg index failed: %v\n", err)
		if langHint == "swift" {
			fmt.Fprintf(os.Stderr, "  Provide compatible scip-swift first, then retry with: gg index --lang %s\n", langHint)
		} else {
			fmt.Fprintf(os.Stderr, "  You can retry with: gg index --lang %s\n", langHint)
		}
		return false
	}
	return true
}
