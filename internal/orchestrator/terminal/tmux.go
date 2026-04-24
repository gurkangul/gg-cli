//go:build tmux

// Package terminal — tmux stub.
// Real implementation is deferred (TASK-275).
// Building with -tags tmux gives a compilable placeholder so the registry
// can list "tmux" as a known backend without panicking.
package terminal

import (
	"context"
	"fmt"
)

type tmuxStub struct{}

func newTmuxStub() *tmuxStub { return &tmuxStub{} }

func (t *tmuxStub) NewSplit(_ context.Context, _ SplitOpts) (SurfaceID, error) {
	return "", fmt.Errorf("tmux adapter not yet implemented (TASK-275): %w", ErrCapabilityUnsupported)
}
func (t *tmuxStub) Send(_ context.Context, _ SurfaceID, _ string) error {
	return fmt.Errorf("tmux adapter not yet implemented (TASK-275): %w", ErrCapabilityUnsupported)
}
func (t *tmuxStub) SendKey(_ context.Context, _ SurfaceID, _ string) error {
	return fmt.Errorf("tmux adapter not yet implemented (TASK-275): %w", ErrCapabilityUnsupported)
}
func (t *tmuxStub) ReadScreen(_ context.Context, _ SurfaceID) ([]byte, error) {
	return nil, fmt.Errorf("tmux adapter not yet implemented (TASK-275): %w", ErrCapabilityUnsupported)
}
func (t *tmuxStub) Focus(_ context.Context, _ SurfaceID) error {
	return fmt.Errorf("tmux adapter not yet implemented (TASK-275): %w", ErrCapabilityUnsupported)
}
func (t *tmuxStub) Close(_ context.Context, _ SurfaceID) error {
	return fmt.Errorf("tmux adapter not yet implemented (TASK-275): %w", ErrCapabilityUnsupported)
}
func (t *tmuxStub) Capabilities() Caps {
	return Caps{CanReadScreen: false}
}
