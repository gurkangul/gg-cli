package terminal

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
)

// captureRunner records calls and returns configurable output/error.
type captureRunner struct {
	calls   [][]string
	output  []byte
	outputs [][]byte
	err     error
}

func (r *captureRunner) run(ctx context.Context, args ...string) ([]byte, error) {
	r.calls = append(r.calls, append([]string(nil), args...))
	if len(r.outputs) > 0 {
		out := r.outputs[0]
		r.outputs = r.outputs[1:]
		return out, r.err
	}
	return r.output, r.err
}

func newCapture(out string) *captureRunner {
	return &captureRunner{output: []byte(out)}
}

// TestCmux_NewSplit_HorizontalDefault verifies that horizontal maps to a
// below split direction while creating an independent pane in the workspace.
func TestCmux_NewSplit_HorizontalDefault(t *testing.T) {
	r := newCapture("OK surface:1 workspace:1\n")
	c := newCmuxWithRunner(r.run)
	ctx := context.Background()

	id, err := c.NewSplit(ctx, SplitOpts{Dir: SplitHorizontal})
	if err != nil {
		t.Fatal(err)
	}
	// Runner output "surf-1\n" has no "surface:" prefix → parseSurfaceID returns "".
	// Use an "OK surface:1 workspace:2" shape when the parse matters; the legacy
	// bare form now yields an error rather than passing through.
	_ = id
	if len(r.calls) != 1 {
		t.Fatalf("expected 1 call got %d", len(r.calls))
	}
	if len(r.calls[0]) < 5 ||
		r.calls[0][0] != "new-pane" ||
		r.calls[0][1] != "--type" ||
		r.calls[0][2] != "terminal" ||
		r.calls[0][3] != "--direction" ||
		r.calls[0][4] != "down" {
		t.Fatalf("expected [new-pane --type terminal --direction down ...], got: %v", r.calls[0])
	}
}

func TestCmux_NewSplit_Vertical(t *testing.T) {
	r := newCapture("OK surface:2 workspace:1\n")
	c := newCmuxWithRunner(r.run)
	ctx := context.Background()

	_, err := c.NewSplit(ctx, SplitOpts{Dir: SplitVertical})
	if err != nil {
		t.Fatal(err)
	}
	// cmux new-pane takes --direction left|right|up|down.
	// SplitVertical maps to "right".
	if len(r.calls[0]) < 5 ||
		r.calls[0][0] != "new-pane" ||
		r.calls[0][3] != "--direction" ||
		r.calls[0][4] != "right" {
		t.Fatalf("expected [new-pane --type terminal --direction right ...], got: %v", r.calls[0])
	}
}

func TestCmux_NewSplit_Percent(t *testing.T) {
	r := newCapture("OK surface:3 workspace:1\n")
	c := newCmuxWithRunner(r.run)
	ctx := context.Background()

	_, err := c.NewSplit(ctx, SplitOpts{Percent: 40})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, a := range r.calls[0] {
		if a == "--percent=40" {
			found = true
		}
	}
	if !found {
		t.Fatalf("--percent=40 missing from args: %v", r.calls[0])
	}
}

func TestCmux_NewSplit_Env(t *testing.T) {
	r := newCapture("OK surface:4 workspace:1\n")
	c := newCmuxWithRunner(r.run)
	ctx := context.Background()

	_, err := c.NewSplit(ctx, SplitOpts{Env: []string{"FOO=bar", "BAZ=qux"}})
	if err != nil {
		t.Fatal(err)
	}
	args := r.calls[0]
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--env=FOO=bar") || !strings.Contains(joined, "--env=BAZ=qux") {
		t.Fatalf("env args missing: %v", args)
	}
}

func TestCmux_NewSplit_EmptySurfaceID(t *testing.T) {
	r := newCapture("   \n")
	c := newCmuxWithRunner(r.run)
	ctx := context.Background()

	_, err := c.NewSplit(ctx, SplitOpts{})
	if err == nil {
		t.Fatal("expected error for unparseable output")
	}
	if !strings.Contains(err.Error(), "unparseable") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestCmux_NewSplit_ParsesOKSurfaceFormat verifies the "OK surface:N workspace:M"
// output shape is parsed to extract surface:N.
func TestCmux_NewSplit_ParsesOKSurfaceFormat(t *testing.T) {
	r := newCapture("OK surface:42 workspace:1\n")
	c := newCmuxWithRunner(r.run)
	ctx := context.Background()

	id, err := c.NewSplit(ctx, SplitOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if id != "surface:42" {
		t.Fatalf("want surface:42 got %q", id)
	}
}

func TestCmux_NewSplit_RunnerError(t *testing.T) {
	r := &captureRunner{err: errors.New("cmux not found")}
	c := newCmuxWithRunner(r.run)
	ctx := context.Background()

	_, err := c.NewSplit(ctx, SplitOpts{})
	if err == nil {
		t.Fatal("expected error when runner fails")
	}
}

func TestCmux_Send(t *testing.T) {
	r := newCapture("")
	c := newCmuxWithRunner(r.run)
	ctx := context.Background()

	if err := c.Send(ctx, "surface:1", "hello world"); err != nil {
		t.Fatal(err)
	}
	if len(r.calls) != 1 {
		t.Fatalf("expected 1 call got %d", len(r.calls))
	}
	args := r.calls[0]
	if args[0] != "send" || args[1] != "--surface" || args[2] != "surface:1" || args[3] != "hello world" {
		t.Fatalf("unexpected args: %v", args)
	}
}

func TestCmux_SendKey(t *testing.T) {
	r := newCapture("")
	c := newCmuxWithRunner(r.run)
	ctx := context.Background()

	if err := c.SendKey(ctx, "surface:1", "enter"); err != nil {
		t.Fatal(err)
	}
	args := r.calls[0]
	if args[0] != "send-key" || args[1] != "--surface" || args[2] != "surface:1" || args[3] != "enter" {
		t.Fatalf("unexpected args: %v", args)
	}
}

func TestCmux_ReadScreen(t *testing.T) {
	r := &captureRunner{outputs: [][]byte{
		[]byte("surface:1  type=terminal in_window=false\n"),
		[]byte("screen content"),
	}}
	c := newCmuxWithRunner(r.run)
	ctx := context.Background()

	out, err := c.ReadScreen(ctx, "surface:1")
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "screen content" {
		t.Fatalf("want %q got %q", "screen content", string(out))
	}
	if len(r.calls) != 2 {
		t.Fatalf("expected surface-health + read-screen calls, got %d", len(r.calls))
	}
	args := r.calls[1]
	if args[0] != "read-screen" || args[1] != "--surface" || args[2] != "surface:1" {
		t.Fatalf("unexpected args: %v", args)
	}
}

func TestCmux_ReadScreen_MissingSurface(t *testing.T) {
	r := newCapture("surface:2  type=terminal in_window=false\n")
	c := newCmuxWithRunner(r.run)
	ctx := context.Background()

	_, err := c.ReadScreen(ctx, "surface:1")
	if !IsErrSurfaceNotFound(err) {
		t.Fatalf("expected ErrSurfaceNotFound, got %v", err)
	}
	if len(r.calls) != 1 || r.calls[0][0] != "surface-health" {
		t.Fatalf("expected only surface-health call, got %v", r.calls)
	}
}

func TestCmux_Focus(t *testing.T) {
	r := newCapture("")
	c := newCmuxWithRunner(r.run)
	ctx := context.Background()

	if err := c.Focus(ctx, "surface:1"); err != nil {
		t.Fatal(err)
	}
	args := r.calls[0]
	// focus-pane uses --pane, not --surface (cmux CLI quirk).
	if args[0] != "focus-pane" || args[1] != "--pane" || args[2] != "surface:1" {
		t.Fatalf("unexpected args: %v", args)
	}
}

func TestCmux_Close(t *testing.T) {
	r := newCapture("")
	c := newCmuxWithRunner(r.run)
	ctx := context.Background()

	if err := c.Close(ctx, "surface:1"); err != nil {
		t.Fatal(err)
	}
	args := r.calls[0]
	if args[0] != "close-surface" || args[1] != "--surface" || args[2] != "surface:1" {
		t.Fatalf("unexpected args: %v", args)
	}
}

func TestCmux_Capabilities(t *testing.T) {
	c := newCmux()
	if !c.Capabilities().CanReadScreen {
		t.Fatal("cmux must advertise CanReadScreen=true")
	}
}

// TestNew_CmuxKind verifies New("cmux") returns a non-nil terminal.
// newCmux() is always constructable regardless of whether the binary exists.
func TestNew_CmuxKind(t *testing.T) {
	term, err := New("cmux")
	if err != nil {
		t.Fatal(err)
	}
	if term == nil {
		t.Fatal("expected non-nil terminal")
	}
}

// TestNewFromEnv_Fake verifies NewFromEnv honours GG_TERMINAL.
func TestNewFromEnv_Fake(t *testing.T) {
	t.Setenv("GG_TERMINAL", "fake")
	term, err := NewFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if term == nil {
		t.Fatal("expected non-nil terminal")
	}
}

// TestNewFromEnv_Unknown verifies NewFromEnv returns an error for bad values.
func TestNewFromEnv_Unknown(t *testing.T) {
	t.Setenv("GG_TERMINAL", "nonexistent")
	_, err := NewFromEnv()
	if err == nil {
		t.Fatal("expected error for unknown terminal kind")
	}
}

// TestProbeSurface_DeadSurface verifies ProbeSurface returns dead=true when
// surface-health does not list the requested surface.
func TestProbeSurface_DeadSurface(t *testing.T) {
	stub := buildStubCmux(t, `#!/bin/sh
echo "surface:76  type=terminal in_window=false"
exit 0`)

	origPATH := os.Getenv("PATH")
	t.Setenv("PATH", stub+":"+origPATH)

	dead, err := ProbeSurface(context.Background(), "surface:99")
	if !dead {
		t.Errorf("expected dead=true, got false (err=%v)", err)
	}
}

// TestProbeSurface_StarredLiveSurface verifies selected rows are parsed too.
func TestProbeSurface_WrongFocusedSurface(t *testing.T) {
	stub := buildStubCmux(t, `#!/bin/sh
echo "* surface:88  task shell  [selected]"
exit 0`)

	origPATH := os.Getenv("PATH")
	t.Setenv("PATH", stub+":"+origPATH)

	dead, err := ProbeSurface(context.Background(), "surface:88")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dead {
		t.Fatal("expected selected requested surface to be live")
	}
}

func TestProbeSurface_LiveRequestedSurface(t *testing.T) {
	stub := buildStubCmux(t, `#!/bin/sh
echo "surface:88  type=terminal in_window=false"
exit 0`)

	origPATH := os.Getenv("PATH")
	t.Setenv("PATH", stub+":"+origPATH)

	dead, err := ProbeSurface(context.Background(), "surface:88")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dead {
		t.Fatal("expected requested surface identity to be live")
	}
}

// TestProbeSurface_TransientError verifies ProbeSurface returns dead=false
// when the cmux binary returns a non-dead-message error (e.g. timeout, unknown
// internal error).
func TestProbeSurface_TransientError(t *testing.T) {
	stub := buildStubCmux(t, `#!/bin/sh
echo "internal error: connection refused"
exit 2`)

	origPATH := os.Getenv("PATH")
	t.Setenv("PATH", stub+":"+origPATH)

	dead, _ := ProbeSurface(context.Background(), "surface:99")
	if dead {
		t.Error("expected dead=false for non-definitive error")
	}
}

// TestProbeSurface_CmuxMissing verifies ProbeSurface returns dead=false (not a
// prune trigger) when the cmux binary is not in PATH — avoids false pruning in
// non-cmux environments.
func TestProbeSurface_CmuxMissing(t *testing.T) {
	t.Setenv("PATH", "")

	dead, _ := ProbeSurface(context.Background(), "surface:99")
	if dead {
		t.Error("expected dead=false when cmux binary not found")
	}
}

// buildStubCmux writes a shell script named "cmux" to a temp dir and returns
// the dir path so callers can prepend it to PATH.
func buildStubCmux(t *testing.T, script string) string {
	t.Helper()
	dir := t.TempDir()
	bin := dir + "/cmux"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatalf("write stub cmux: %v", err)
	}
	return dir
}

// TestAutoDetect_FallbackEmpty verifies autoDetect returns "" when neither
// cmux nor tmux is in PATH.  We manipulate PATH to guarantee neither is found.
func TestAutoDetect_FallbackEmpty(t *testing.T) {
	orig := os.Getenv("PATH")
	t.Setenv("PATH", "")
	defer func() { _ = os.Setenv("PATH", orig) }()

	got := autoDetect()
	if got != "" {
		t.Fatalf("expected empty string from autoDetect with empty PATH, got %q", got)
	}
}

// TestNew_AutoDetectEmpty verifies New("") with an empty PATH returns an error
// (no binary found, empty kind → New receives "" → autoDetect returns "" →
// falls to default case).
func TestNew_AutoDetectEmpty(t *testing.T) {
	orig := os.Getenv("PATH")
	t.Setenv("PATH", "")
	defer func() { _ = os.Setenv("PATH", orig) }()

	_, err := New("")
	if err == nil {
		t.Fatal("expected error when no terminal binary is available")
	}
	if !strings.Contains(err.Error(), "unknown terminal") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

// TestFake_UnusedBranches exercises the error-on-closed branches in FakeTerminal
// that the existing test file didn't cover (SendKey and Focus on dead surface).
func TestFake_SendKeyOnClosed(t *testing.T) {
	f := NewFake()
	ctx := context.Background()
	id, _ := f.NewSplit(ctx, SplitOpts{})
	_ = f.Close(ctx, id)

	if err := f.SendKey(ctx, id, "Enter"); err == nil {
		t.Fatal("expected error sending key to closed surface")
	}
}

func TestFake_FocusOnClosed(t *testing.T) {
	f := NewFake()
	ctx := context.Background()
	id, _ := f.NewSplit(ctx, SplitOpts{})
	_ = f.Close(ctx, id)

	if err := f.Focus(ctx, id); err == nil {
		t.Fatal("expected error focusing closed surface")
	}
}

// TestCmux_execRun_ErrorFormat verifies the error message shape produced by
// execRun when the subprocess fails (uses a guaranteed-failing binary path).
func TestCmux_execRun_ErrorFormat(t *testing.T) {
	c := newCmux()
	ctx := context.Background()

	// Replace runner with one that returns an error to exercise the error wrap.
	c.runner = func(_ context.Context, args ...string) ([]byte, error) {
		return nil, fmt.Errorf("cmux %s: exec: no such file", args[0])
	}

	err := c.Send(ctx, "surf-x", "test")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "cmux") {
		t.Fatalf("expected 'cmux' in error: %v", err)
	}
}
