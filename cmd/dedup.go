package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
)

const (
	dupThreshold = float32(0.85)
	dupLimit     = uint64(3)
)

// promptIfDuplicate searches for near-duplicates in the given kind's collection.
// If candidates are found:
//   - In a TTY: prints them and prompts the user to continue or abort.
//   - In non-TTY (agent / pipe): prints a warning to stderr and proceeds.
//
// Returns true if the caller should abort creation.
// Errors from the dedup search are treated as non-fatal: a warning is printed
// and creation continues, so a transient Qdrant glitch never blocks writes.
func promptIfDuplicate(ctx context.Context, d *deps, kind string, vector []float32) (abort bool) {
	cands, err := d.store.FindNearDups(ctx, kind, vector, dupThreshold, dupLimit)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: dedup check failed: %v\n", err)
		return false
	}
	if len(cands) == 0 {
		return false
	}

	fmt.Fprintf(os.Stderr, "⚠  Similar %s already exist:\n", kind)
	for _, c := range cands {
		label := c.Label
		if len(label) > 72 {
			label = label[:69] + "..."
		}
		fmt.Fprintf(os.Stderr, "   %-12s  %q  (similarity: %.0f%%)\n", c.ID, label, c.Score*100)
	}

	if !isTerminal(os.Stdin) {
		fmt.Fprintln(os.Stderr, "   (non-interactive — creating anyway)")
		return false
	}

	fmt.Fprint(os.Stderr, "Create anyway? [y/N]: ")
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer != "y" && answer != "yes"
}

// isTerminal reports whether f is a character device (i.e. a real terminal).
func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}
