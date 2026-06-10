package cmd

import (
	"fmt"
	"os"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/projectstate"
)

// recordTaskFullHydration records the projectstate hydration PROOF that a full
// (non-compact) `gg task get` read happened — this is what the hydration gate
// checks before allowing ready-for-live/done/block (BUG-074). It is distinct
// from the telemetry hydration entry emitted by emitHydration: this one is the
// gate's durable "the worker actually read the spec" marker, the telemetry one
// feeds the dogfood re-fetch metric. Errors are surfaced on stderr (never
// swallowed) but do not abort the read.
//
// Extracted from task_list.go (TASK-491) to keep that file under the 500-line
// source cap once the gate-mandated hydration plumbing was added.
func recordTaskFullHydration(taskID string) {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "⚠ could not record task hydration proof: %v\n", err)
		return
	}
	runtimeDir, err := cfg.RuntimeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "⚠ could not record task hydration proof: %v\n", err)
		return
	}
	if err := projectstate.RecordHydration(runtimeDir, "task", taskID); err != nil {
		fmt.Fprintf(os.Stderr, "⚠ could not record task hydration proof: %v\n", err)
	}
}
