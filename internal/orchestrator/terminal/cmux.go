package terminal

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
)

// cmuxTerminal drives the cmux CLI binary.
// It shells out to: cmux new-split, cmux send, cmux send-key,
// cmux read-screen, cmux focus-pane, cmux close-surface.
type cmuxTerminal struct {
	mu     sync.Mutex
	next   atomic.Int64
	runner func(ctx context.Context, args ...string) ([]byte, error)
}

func newCmux() *cmuxTerminal {
	c := &cmuxTerminal{}
	c.runner = c.execRun
	return c
}

// newCmuxWithRunner returns a cmuxTerminal with a custom runner, for unit testing.
func newCmuxWithRunner(r func(ctx context.Context, args ...string) ([]byte, error)) *cmuxTerminal {
	return &cmuxTerminal{runner: r}
}

func (c *cmuxTerminal) NewSplit(ctx context.Context, opts SplitOpts) (SurfaceID, error) {
	// cmux new-split requires a positional direction: left|right|up|down.
	// Map SplitDir: vertical → "right" (side-by-side), horizontal → "down" (above/below).
	dir := "right"
	if opts.Dir == SplitHorizontal {
		dir = "down"
	}
	args := []string{"new-split", dir}
	if opts.Percent > 0 {
		args = append(args, fmt.Sprintf("--percent=%d", opts.Percent))
	}
	for _, e := range opts.Env {
		args = append(args, "--env="+e)
	}
	// Note: cmux new-split does not accept a trailing command — opts.Cmd is
	// delivered via Send() after the pane opens (see cmd/spawn_worker.go).
	out, err := c.runner(ctx, args...)
	if err != nil {
		return "", err
	}
	// cmux new-split output shape: "OK surface:N workspace:M\n".
	// Extract the surface:N token so downstream --surface=<id> calls work.
	id := parseSurfaceID(string(out))
	if id == "" {
		return "", fmt.Errorf("cmux new-split returned unparseable output: %q", strings.TrimSpace(string(out)))
	}
	return id, nil
}

// parseSurfaceID extracts the "surface:N" ref from cmux new-split output.
// Output format: "OK surface:29 workspace:1" (or bare "surface:29" in older builds).
func parseSurfaceID(out string) SurfaceID {
	for _, tok := range strings.Fields(out) {
		if strings.HasPrefix(tok, "surface:") {
			return SurfaceID(tok)
		}
	}
	return ""
}

// cmux CLI quirk: --flag=value form mis-parses (treats positional args as the
// flag value). Pass --flag and value as separate tokens instead.

func (c *cmuxTerminal) Send(ctx context.Context, id SurfaceID, text string) error {
	_, err := c.runner(ctx, "send", "--surface", string(id), text)
	return err
}

func (c *cmuxTerminal) SendKey(ctx context.Context, id SurfaceID, key string) error {
	_, err := c.runner(ctx, "send-key", "--surface", string(id), key)
	return err
}

func (c *cmuxTerminal) ReadScreen(ctx context.Context, id SurfaceID) ([]byte, error) {
	out, err := c.runner(ctx, "read-screen", "--surface", string(id))
	return out, err
}

func (c *cmuxTerminal) Focus(ctx context.Context, id SurfaceID) error {
	// Note: focus-pane uses --pane flag (not --surface like the other ops).
	_, err := c.runner(ctx, "focus-pane", "--pane", string(id))
	return err
}

func (c *cmuxTerminal) Close(ctx context.Context, id SurfaceID) error {
	_, err := c.runner(ctx, "close-surface", "--surface", string(id))
	return err
}

func (c *cmuxTerminal) Capabilities() Caps {
	return Caps{CanReadScreen: true}
}

func (c *cmuxTerminal) execRun(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "cmux", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("cmux %s: %s", args[0], msg)
	}
	return out, nil
}
