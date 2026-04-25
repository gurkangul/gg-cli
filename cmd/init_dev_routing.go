package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gurkangul/gg-cli/internal/agenthooks"
)

// printRolesSection writes the "Roles:" block to os.Stdout.
// Printing the queue-hint line here (rather than in runInit) keeps the roles
// display self-contained: the block always shows dev-routing status and queue
// hint together, regardless of where in the init flow it is called.
func printRolesSection(projectRoot string) {
	fmt.Println("Roles:")
	devRoutingMsg, devRoutingErr := runInitDevRouting(projectRoot)
	if devRoutingErr != nil {
		fmt.Fprintf(os.Stderr, "⚠ dev-routing: %v\n", devRoutingErr)
	} else {
		fmt.Printf("  %s\n", devRoutingMsg)
	}
	fmt.Println(initQueueLine)
}

// runInitDevRouting installs the dev-routing managed block in CLAUDE.md with
// brownfield safety: if CLAUDE.md already exists, is non-empty, and does not
// contain the dev-routing marker, we print a warning and skip rather than
// silently appending to content the user may have customised.
//
// Returns the Roles-section line to print (always non-empty on success or skip).
func runInitDevRouting(projectRoot string) (string, error) {
	claudeMD := filepath.Join(projectRoot, "CLAUDE.md")

	raw, err := os.ReadFile(claudeMD)
	if err == nil && len(strings.TrimSpace(string(raw))) > 0 {
		// File exists and is non-empty.
		if !strings.Contains(string(raw), agenthooks.DevRoutingBlockBegin) {
			// Brownfield: pre-existing content without the managed marker.
			// Do NOT append — print a hint instead (AC-2).
			return "⚠ dev-routing skipped (CLAUDE.md pre-existing — run gg doctor --check-dev-routing --fix)", nil
		}
		// Marker already present — call FixDevRouting for idempotent update.
	}
	// Either CLAUDE.md is missing, empty, or already has the marker.
	lines, fixErr := agenthooks.FixDevRouting(projectRoot, false)
	if fixErr != nil {
		return "", fmt.Errorf("dev-routing: %w", fixErr)
	}

	// Build a compact summary from FixDevRouting's per-action lines.
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if strings.Contains(l, "MISSING") || strings.Contains(l, "STALE") ||
			strings.Contains(l, "✓") {
			return "✓ dev-routing installed in CLAUDE.md", nil
		}
		if strings.Contains(l, "no action needed") || strings.Contains(l, "OK") {
			return "✓ dev-routing already up-to-date in CLAUDE.md", nil
		}
		if strings.Contains(l, "DRIFTED") {
			return "⚠ dev-routing skipped (CLAUDE.md markers malformed — run gg doctor --check-dev-routing --fix)", nil
		}
	}
	return "✓ dev-routing installed in CLAUDE.md", nil
}
