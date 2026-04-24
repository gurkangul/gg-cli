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
	mu   sync.Mutex
	next atomic.Int64
}

func newCmux() *cmuxTerminal {
	return &cmuxTerminal{}
}

func (c *cmuxTerminal) NewSplit(ctx context.Context, opts SplitOpts) (SurfaceID, error) {
	args := []string{"new-split"}
	if opts.Dir == SplitVertical {
		args = append(args, "--vertical")
	}
	if opts.Percent > 0 {
		args = append(args, fmt.Sprintf("--percent=%d", opts.Percent))
	}
	for _, e := range opts.Env {
		args = append(args, "--env="+e)
	}
	if opts.Cmd != "" {
		args = append(args, "--", opts.Cmd)
	}
	out, err := c.run(ctx, args...)
	if err != nil {
		return "", err
	}
	id := SurfaceID(strings.TrimSpace(string(out)))
	if id == "" {
		return "", fmt.Errorf("cmux new-split returned empty surface id")
	}
	return id, nil
}

func (c *cmuxTerminal) Send(ctx context.Context, id SurfaceID, text string) error {
	_, err := c.run(ctx, "send", string(id), text)
	return err
}

func (c *cmuxTerminal) SendKey(ctx context.Context, id SurfaceID, key string) error {
	_, err := c.run(ctx, "send-key", string(id), key)
	return err
}

func (c *cmuxTerminal) ReadScreen(ctx context.Context, id SurfaceID) ([]byte, error) {
	out, err := c.run(ctx, "read-screen", string(id))
	return out, err
}

func (c *cmuxTerminal) Focus(ctx context.Context, id SurfaceID) error {
	_, err := c.run(ctx, "focus-pane", string(id))
	return err
}

func (c *cmuxTerminal) Close(ctx context.Context, id SurfaceID) error {
	_, err := c.run(ctx, "close-surface", string(id))
	return err
}

func (c *cmuxTerminal) Capabilities() Caps {
	return Caps{CanReadScreen: true}
}

func (c *cmuxTerminal) run(ctx context.Context, args ...string) ([]byte, error) {
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
