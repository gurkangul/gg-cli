package cmd

import (
	"encoding/json"
	"fmt"
	"os"
)

// jsonOutput is set by the root --json persistent flag.
var jsonOutput bool

// Exit code constants — callers can check the exit code to distinguish error classes.
//
//   0  success
//   1  general error
//   2  resource not found
//   3  config / init error (run `gg init`)
//   4  service unreachable (Qdrant / Ollama / Memgraph)
//   130 interrupted (Ctrl+C)
const (
	ExitOK      = 0
	ExitGeneral = 1
	ExitNotFound = 2
	ExitConfig  = 3
	ExitService = 4
	ExitSignal  = 130
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

// jsonError writes a structured JSON error object to stdout and returns the
// original error so the caller can still return it to cobra.
//
// Format: {"error": "message", "code": N}
func jsonError(err error, code int) error {
	_ = writeJSON(map[string]any{
		"error": err.Error(),
		"code":  code,
	})
	return err
}

// printlnf is a convenience wrapper — routes to fmt.Printf but lets future
// callers swap it for a buffered writer without touching every command.
func printlnf(format string, args ...any) {
	fmt.Printf(format, args...)
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
