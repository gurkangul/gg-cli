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
	args := []string{"--id-format", "both", "new-pane", "--type", "terminal", "--direction", dir}
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
	// cmux new-pane output shape: "OK surface:N (...) pane:N (...) workspace:M (...)\n".
	// Store workspace/surface when cmux exposes both UUIDs so downstream calls
	// can address panes from a different master workspace.
	id := parseSurfaceID(string(out))
	if id == "" {
		return "", fmt.Errorf("cmux new-split returned unparseable output: %q", strings.TrimSpace(string(out)))
	}
	return id, nil
}

// parseSurfaceID extracts a cmux surface ref from new-pane output.
// When --id-format both includes UUIDs for both workspace and surface, return
// "workspaceUUID/surfaceUUID"; cmux needs --workspace for cross-workspace
// send/read-screen calls. Older output falls back to the surface token.
func parseSurfaceID(out string) SurfaceID {
	tokens := strings.Fields(out)
	var surface string
	var workspace string
	for i, tok := range tokens {
		if strings.HasPrefix(tok, "surface:") {
			if i+1 < len(tokens) && strings.HasPrefix(tokens[i+1], "(") && strings.HasSuffix(tokens[i+1], ")") {
				uuid := strings.TrimSuffix(strings.TrimPrefix(tokens[i+1], "("), ")")
				if uuid != "" {
					surface = uuid
					continue
				}
			}
			surface = tok
			continue
		}
		if strings.HasPrefix(tok, "workspace:") {
			if i+1 < len(tokens) && strings.HasPrefix(tokens[i+1], "(") && strings.HasSuffix(tokens[i+1], ")") {
				uuid := strings.TrimSuffix(strings.TrimPrefix(tokens[i+1], "("), ")")
				if uuid != "" {
					workspace = uuid
				}
			}
		}
	}
	if surface == "" {
		return ""
	}
	if workspace != "" && !strings.HasPrefix(surface, "surface:") {
		return SurfaceID(workspace + "/" + surface)
	}
	return SurfaceID(surface)
}

// cmux CLI quirk: --flag=value form mis-parses (treats positional args as the
// flag value). Pass --flag and value as separate tokens instead.

func (c *cmuxTerminal) Send(ctx context.Context, id SurfaceID, text string) error {
	args := append([]string{"send"}, cmuxSurfaceArgs(id)...)
	args = append(args, text)
	_, err := c.runner(ctx, args...)
	return err
}

func (c *cmuxTerminal) SendKey(ctx context.Context, id SurfaceID, key string) error {
	args := append([]string{"send-key"}, cmuxSurfaceArgs(id)...)
	args = append(args, key)
	_, err := c.runner(ctx, args...)
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
	args := append([]string{"read-screen"}, cmuxSurfaceArgs(id)...)
	out, err := c.runner(ctx, args...)
	return out, err
}

func (c *cmuxTerminal) surfaceExists(ctx context.Context, id SurfaceID) (bool, error) {
	out, err := c.runner(ctx, surfaceHealthArgs(id)...)
	if err != nil {
		return false, err
	}
	return surfaceHealthContains(out, id), nil
}

func (c *cmuxTerminal) Focus(ctx context.Context, id SurfaceID) error {
	// Note: focus-pane uses --pane flag (not --surface like the other ops).
	workspace, surface := splitCmuxScopedID(id)
	args := []string{"focus-pane"}
	if workspace != "" {
		args = append(args, "--workspace", workspace)
	}
	args = append(args, "--pane", surface)
	_, err := c.runner(ctx, args...)
	return err
}

func (c *cmuxTerminal) Close(ctx context.Context, id SurfaceID) error {
	args := append([]string{"close-surface"}, cmuxSurfaceArgs(id)...)
	_, err := c.runner(ctx, args...)
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

	args := surfaceHealthArgs(id)
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

func surfaceHealthArgs(id SurfaceID) []string {
	args := []string{"--id-format", "both", "surface-health"}
	workspace, _ := splitCmuxScopedID(id)
	if workspace == "" {
		workspace = os.Getenv("CMUX_WORKSPACE_ID")
	}
	if workspace != "" {
		args = append(args, "--workspace", workspace)
	}
	return args
}

func surfaceHealthContains(out []byte, id SurfaceID) bool {
	_, requested := splitCmuxScopedID(id)
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
		if len(fields) > 1 && strings.Trim(fields[1], "()") == requested {
			return true
		}
	}
	return false
}

func cmuxSurfaceArgs(id SurfaceID) []string {
	workspace, surface := splitCmuxScopedID(id)
	args := []string{}
	if workspace != "" {
		args = append(args, "--workspace", workspace)
	}
	return append(args, "--surface", surface)
}

func splitCmuxScopedID(id SurfaceID) (workspace string, surface string) {
	raw := string(id)
	parts := strings.SplitN(raw, "/", 2)
	if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
		return parts[0], parts[1]
	}
	return "", raw
}
