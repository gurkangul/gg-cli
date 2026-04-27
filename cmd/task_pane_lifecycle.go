package cmd

import (
	"context"
	"fmt"
	"io"

	"github.com/gurkangul/gg-cli/internal/orchestrator/spawn"
	"github.com/gurkangul/gg-cli/internal/orchestrator/terminal"
)

// closeWorkerPaneForTask closes the terminal pane registered for taskID in
// panes.json and removes the registry entry atomically under flock.
//
// Contract (AC-1/AC-2):
//   - Looks up the pane entry; no-ops silently when none exists (idempotent).
//   - Calls cmux close-surface for the registered surface; on failure logs a
//     single [pane-lifecycle] line to errOut and continues — the data side
//     (store update) already succeeded.
//   - Removes the panes.json entry under flock.
//   - Prints "✓ closed pane <surface> for <task>" to stdout on success.
func closeWorkerPaneForTask(ctx context.Context, rt, taskID string, stdout, errOut io.Writer) {
	pane, err := spawn.FindPaneForTask(rt, taskID)
	if err != nil || pane == nil {
		return // no entry — nothing to close (idempotent)
	}

	surfaceID := terminal.SurfaceID(pane.SurfaceID)

	term, termErr := terminal.NewFromEnv()
	if termErr == nil {
		if closeErr := term.Close(ctx, surfaceID); closeErr != nil {
			fmt.Fprintf(errOut, "[pane-lifecycle] close %s for %s: %v\n", pane.SurfaceID, taskID, closeErr)
		}
	} else {
		fmt.Fprintf(errOut, "[pane-lifecycle] terminal backend unavailable for %s: %v\n", taskID, termErr)
	}

	// Remove from panes.json regardless of whether the terminal close succeeded.
	if removeErr := spawn.RemovePaneLocked(rt, taskID); removeErr != nil {
		fmt.Fprintf(errOut, "[pane-lifecycle] remove panes.json entry for %s: %v\n", taskID, removeErr)
		return
	}

	fmt.Fprintf(stdout, "✓ closed pane %s for %s\n", pane.SurfaceID, taskID)
}
