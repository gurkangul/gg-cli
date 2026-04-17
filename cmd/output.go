package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/telemetry"
)

// jsonOutput is set by the root --json persistent flag.
var jsonOutput bool

// Exit code constants — callers can check the exit code to distinguish error classes.
//
//   0   success
//   1   general error
//   2   resource not found
//   3   config / init error (run `gg init`)
//   4   service unreachable (Qdrant / Ollama / Memgraph)
//   6   store down — writes blocked, reads served from cache
//   130 interrupted (Ctrl+C)
const (
	ExitOK         = 0
	ExitGeneral    = 1
	ExitNotFound   = 2
	ExitConfig     = 3
	ExitService    = 4
	ExitStoreDown  = 6  // Qdrant unreachable: write commands fail, read commands serve cached results
	ExitSignal     = 130
)

// ExitError is a sentinel that carries a structured exit code alongside the
// human-readable message. Commands return an *ExitError to signal
// machine-detectable failure categories. Execute() unwraps it to set the
// process exit code.
type ExitError struct {
	Code    int
	Message string
}

func (e *ExitError) Error() string { return e.Message }

// notFound wraps a "not found" message in an ExitError with code 2.
func notFound(msg string) error { return &ExitError{Code: ExitNotFound, Message: msg} }

// configErr wraps a config error message in an ExitError with code 3.
func configErr(msg string) error { return &ExitError{Code: ExitConfig, Message: msg} }

// serviceErr wraps a service unreachable error in an ExitError with code 4.
func serviceErr(msg string) error { return &ExitError{Code: ExitService, Message: msg} }

// writeJSON serialises v as indented JSON to stdout.
func writeJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// printJSON writes v as JSON if --json is set, otherwise calls the fallback.
// This is the primary toggle point for the --json flag.
func printJSON(v any, fallback func()) error {
	if jsonOutput {
		return writeJSON(v)
	}
	fallback()
	return nil
}

// emitCompact renders both default and compact views to measure the byte
// savings, prints the compact view, and records a telemetry entry with both
// sizes so `gg status` can surface the dogfood savings metric.
//
// Overhead: the default render runs into a buffer and is discarded — a few
// hundred microseconds on realistic bundles. Only triggered when --compact
// is active, so non-compact calls pay nothing.
func emitCompact(cmd *cobra.Command, verb string, renderDefault, renderCompact func(io.Writer)) {
	var baseline bytes.Buffer
	renderDefault(&baseline)

	var out bytes.Buffer
	renderCompact(&out)
	_, _ = os.Stdout.Write(out.Bytes())

	cfg, err := config.Load()
	if err != nil {
		return
	}
	runtimeDir, err := cfg.RuntimeDir()
	if err != nil {
		return
	}
	fromFlag := ""
	if f := cmd.Flags().Lookup("from"); f != nil {
		fromFlag = f.Value.String()
	}
	telemetry.RecordCompact(runtimeDir, verb, fromFlag, out.Len(), baseline.Len())
}
