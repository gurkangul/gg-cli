package terminal

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// tmuxTerminal drives the tmux CLI binary.
// Surface IDs are tmux pane IDs (e.g. "%3").
type tmuxTerminal struct {
	runner func(ctx context.Context, args ...string) ([]byte, error)
}

func newTmux() *tmuxTerminal {
	t := &tmuxTerminal{}
	t.runner = t.execRun
	return t
}

// newTmuxWithRunner returns a tmuxTerminal with a custom runner, for unit testing.
func newTmuxWithRunner(r func(ctx context.Context, args ...string) ([]byte, error)) *tmuxTerminal {
	return &tmuxTerminal{runner: r}
}

func (t *tmuxTerminal) NewSplit(ctx context.Context, opts SplitOpts) (SurfaceID, error) {
	// tmux split-window: -h = horizontal split (side by side), default = vertical (above/below).
	// Our convention: SplitVertical = side-by-side → -h; SplitHorizontal = above/below → no flag.
	args := []string{"split-window", "-P", "-F", "#{pane_id}"}
	if opts.Dir == SplitVertical {
		args = append(args, "-h")
	}
	if opts.Percent > 0 {
		args = append(args, fmt.Sprintf("-p%d", opts.Percent))
	}
	for _, e := range opts.Env {
		args = append(args, "-e", e)
	}
	if opts.Cmd != "" {
		args = append(args, opts.Cmd)
	}
	out, err := t.runner(ctx, args...)
	if err != nil {
		return "", err
	}
	id := strings.TrimSpace(string(out))
	if id == "" {
		return "", fmt.Errorf("tmux split-window returned empty pane ID")
	}
	return SurfaceID(id), nil
}

func (t *tmuxTerminal) Send(ctx context.Context, id SurfaceID, text string) error {
	// The trailing "" is a literal key that prevents tmux interpreting the text as a key name.
	_, err := t.runner(ctx, "send-keys", "-t", string(id), text, "")
	return err
}

func (t *tmuxTerminal) SendKey(ctx context.Context, id SurfaceID, key string) error {
	_, err := t.runner(ctx, "send-keys", "-t", string(id), key)
	return err
}

func (t *tmuxTerminal) ReadScreen(ctx context.Context, id SurfaceID) ([]byte, error) {
	// capture-pane -p writes pane content to stdout.
	return t.runner(ctx, "capture-pane", "-p", "-t", string(id))
}

func (t *tmuxTerminal) Focus(ctx context.Context, id SurfaceID) error {
	_, err := t.runner(ctx, "select-pane", "-t", string(id))
	return err
}

func (t *tmuxTerminal) Close(ctx context.Context, id SurfaceID) error {
	_, err := t.runner(ctx, "kill-pane", "-t", string(id))
	return err
}

func (t *tmuxTerminal) Capabilities() Caps {
	return Caps{CanReadScreen: true}
}

func (t *tmuxTerminal) execRun(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "tmux", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("tmux %s: %s", args[0], msg)
	}
	return out, nil
}
