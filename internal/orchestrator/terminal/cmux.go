package terminal

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// cmuxTerminal drives the cmux CLI binary.
// It shells out to: cmux new-split, cmux send, cmux send-key,
// cmux read-screen, cmux focus-pane, cmux close-surface.
type cmuxTerminal struct {
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
	// cmux new-pane creates an independent terminal pane in the caller's
	// workspace. Using new-split here couples worker launch to the caller's
	// current surface, which can make task workers look like they were invoked
	// inside the same agent session instead of as a dedicated pane.
	// Map SplitDir: vertical → "right" (side-by-side), horizontal → "down" (above/below).
	dir := "right"
	if opts.Dir == SplitHorizontal {
		dir = "down"
	}
	args := []string{"new-pane", "--type", "terminal", "--direction", dir}
	if opts.Percent > 0 {
		args = append(args, fmt.Sprintf("--percent=%d", opts.Percent))
	}
	for _, e := range opts.Env {
		args = append(args, "--env="+e)
	}
	// Note: cmux new-pane does not accept a trailing command — opts.Cmd is
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
	exists, err := c.surfaceExists(ctx, id)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("%w: %s", ErrSurfaceNotFound, id)
	}
	out, err := c.runner(ctx, "read-screen", "--surface", string(id))
	return out, err
}

func (c *cmuxTerminal) surfaceExists(ctx context.Context, id SurfaceID) (bool, error) {
	out, err := c.runner(ctx, surfaceHealthArgs()...)
	if err != nil {
		return false, err
	}
	return surfaceHealthContains(out, id), nil
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

// ProbeSurface checks cmux surface-health with a 5-second deadline. It returns
// dead=true when the requested surface is not listed in the caller workspace.
// cmux identify/read-screen can fall back to the focused surface for unknown
// IDs, so liveness must be based on exact surface-health membership instead.
//
// Timeouts and other transient errors return dead=false so callers do not
// prune live panes due to a slow cmux response.
func ProbeSurface(ctx context.Context, id SurfaceID) (dead bool, err error) {
	probe, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	args := surfaceHealthArgs()
	cmd := exec.CommandContext(probe, "cmux", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	if runErr != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = runErr.Error()
		}
		return false, fmt.Errorf("cmux surface-health: %s", msg)
	}
	return !surfaceHealthContains(stdout.Bytes(), id), nil
}

func surfaceHealthArgs() []string {
	args := []string{"surface-health"}
	if workspace := os.Getenv("CMUX_WORKSPACE_ID"); workspace != "" {
		args = append(args, "--workspace", workspace)
	}
	return args
}

func surfaceHealthContains(out []byte, id SurfaceID) bool {
	requested := string(id)
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		candidate := fields[0]
		if candidate == "*" && len(fields) > 1 {
			candidate = fields[1]
		}
		if strings.TrimPrefix(candidate, "*") == requested {
			return true
		}
	}
	return false
}
