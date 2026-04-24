// Package terminal provides a swappable backend for pane/terminal management
// in the multi-agent orchestrator.  Callers depend only on the Terminal
// interface; the concrete implementation (cmux, fake, tmux) is selected at
// runtime via the GG_TERMINAL env var or auto-detection.
package terminal

import (
	"context"
	"fmt"
	"os"
	"os/exec"
)

// SurfaceID is an opaque handle for a pane / split returned by NewSplit.
type SurfaceID string

// SplitDir controls how the new pane is created relative to the current one.
type SplitDir string

const (
	SplitHorizontal SplitDir = "horizontal"
	SplitVertical   SplitDir = "vertical"
)

// SplitOpts configures the new pane requested by NewSplit.
type SplitOpts struct {
	Dir     SplitDir
	Percent int    // size percentage (0 = backend default)
	Cmd     string // optional command to run inside the new pane
	Env     []string
}

// Caps reports optional features supported by a Terminal implementation.
type Caps struct {
	CanReadScreen bool
}

// Terminal is the abstraction every orchestrator component programs against.
// Implementations must be safe for concurrent use.
type Terminal interface {
	// NewSplit creates a new pane and returns its opaque surface ID.
	NewSplit(ctx context.Context, opts SplitOpts) (SurfaceID, error)
	// Send writes text to the pane (as if typed).
	Send(ctx context.Context, id SurfaceID, text string) error
	// SendKey sends a special key (e.g. "Enter", "C-c").
	SendKey(ctx context.Context, id SurfaceID, key string) error
	// ReadScreen returns the current visible content of the pane.
	// Returns ErrCapabilityUnsupported when Caps.CanReadScreen is false.
	ReadScreen(ctx context.Context, id SurfaceID) ([]byte, error)
	// Focus brings the pane into view / makes it active.
	Focus(ctx context.Context, id SurfaceID) error
	// Close destroys the pane.
	Close(ctx context.Context, id SurfaceID) error
	// Capabilities reports what this backend can do.
	Capabilities() Caps
}

// ErrCapabilityUnsupported is returned when a method is called on a backend
// that does not support the corresponding capability.
var ErrCapabilityUnsupported = fmt.Errorf("capability unsupported by this terminal backend")

// ErrSurfaceNotFound is returned when the supplied SurfaceID is unknown.
var ErrSurfaceNotFound = fmt.Errorf("surface not found")

// New resolves a Terminal from ggTerminalEnv ("cmux", "tmux", "fake").
// If kind is empty it tries auto-detection via exec.LookPath.
func New(kind string) (Terminal, error) {
	if kind == "" {
		kind = autoDetect()
	}
	switch kind {
	case "cmux":
		return newCmux(), nil
	case "fake":
		return NewFake(), nil
	case "tmux":
		return newTmux(), nil
	default:
		return nil, fmt.Errorf("unknown terminal: %q, supported: cmux, fake, tmux", kind)
	}
}

// NewFromEnv calls New with the value of GG_TERMINAL (empty = auto-detect).
func NewFromEnv() (Terminal, error) {
	return New(os.Getenv("GG_TERMINAL"))
}

func autoDetect() string {
	if _, err := exec.LookPath("cmux"); err == nil {
		return "cmux"
	}
	if _, err := exec.LookPath("tmux"); err == nil {
		return "tmux"
	}
	return ""
}
