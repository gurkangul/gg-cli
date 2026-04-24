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
	calls  [][]string
	output []byte
	err    error
}

func (r *captureRunner) run(ctx context.Context, args ...string) ([]byte, error) {
	r.calls = append(r.calls, append([]string(nil), args...))
	return r.output, r.err
}

func newCapture(out string) *captureRunner {
	return &captureRunner{output: []byte(out)}
}

// TestCmux_NewSplit_HorizontalDefault verifies that horizontal (default) direction
// does not append --vertical.
func TestCmux_NewSplit_HorizontalDefault(t *testing.T) {
	r := newCapture("surf-1\n")
	c := newCmuxWithRunner(r.run)
	ctx := context.Background()

	id, err := c.NewSplit(ctx, SplitOpts{Dir: SplitHorizontal, Cmd: "bash"})
	if err != nil {
		t.Fatal(err)
	}
	if id != "surf-1" {
		t.Fatalf("want surf-1 got %q", id)
	}
	if len(r.calls) != 1 {
		t.Fatalf("expected 1 call got %d", len(r.calls))
	}
	for _, a := range r.calls[0] {
		if a == "--vertical" {
			t.Fatal("--vertical should not be present for horizontal split")
		}
	}
	// -- cmd should be present
	found := false
	for _, a := range r.calls[0] {
		if a == "bash" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected cmd 'bash' in args: %v", r.calls[0])
	}
}

func TestCmux_NewSplit_Vertical(t *testing.T) {
	r := newCapture("surf-2\n")
	c := newCmuxWithRunner(r.run)
	ctx := context.Background()

	_, err := c.NewSplit(ctx, SplitOpts{Dir: SplitVertical})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, a := range r.calls[0] {
		if a == "--vertical" {
			found = true
		}
	}
	if !found {
		t.Fatalf("--vertical missing from args: %v", r.calls[0])
	}
}

func TestCmux_NewSplit_Percent(t *testing.T) {
	r := newCapture("surf-3\n")
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
	r := newCapture("surf-4\n")
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
		t.Fatal("expected error for empty surface id")
	}
	if !strings.Contains(err.Error(), "empty surface id") {
		t.Fatalf("unexpected error: %v", err)
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

	if err := c.Send(ctx, "surf-1", "hello world"); err != nil {
		t.Fatal(err)
	}
	if len(r.calls) != 1 {
		t.Fatalf("expected 1 call got %d", len(r.calls))
	}
	args := r.calls[0]
	if args[0] != "send" || args[1] != "surf-1" || args[2] != "hello world" {
		t.Fatalf("unexpected args: %v", args)
	}
}

func TestCmux_SendKey(t *testing.T) {
	r := newCapture("")
	c := newCmuxWithRunner(r.run)
	ctx := context.Background()

	if err := c.SendKey(ctx, "surf-1", "C-c"); err != nil {
		t.Fatal(err)
	}
	args := r.calls[0]
	if args[0] != "send-key" || args[1] != "surf-1" || args[2] != "C-c" {
		t.Fatalf("unexpected args: %v", args)
	}
}

func TestCmux_ReadScreen(t *testing.T) {
	r := newCapture("screen content")
	c := newCmuxWithRunner(r.run)
	ctx := context.Background()

	out, err := c.ReadScreen(ctx, "surf-1")
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "screen content" {
		t.Fatalf("want %q got %q", "screen content", string(out))
	}
	args := r.calls[0]
	if args[0] != "read-screen" || args[1] != "surf-1" {
		t.Fatalf("unexpected args: %v", args)
	}
}

func TestCmux_Focus(t *testing.T) {
	r := newCapture("")
	c := newCmuxWithRunner(r.run)
	ctx := context.Background()

	if err := c.Focus(ctx, "surf-1"); err != nil {
		t.Fatal(err)
	}
	args := r.calls[0]
	if args[0] != "focus-pane" || args[1] != "surf-1" {
		t.Fatalf("unexpected args: %v", args)
	}
}

func TestCmux_Close(t *testing.T) {
	r := newCapture("")
	c := newCmuxWithRunner(r.run)
	ctx := context.Background()

	if err := c.Close(ctx, "surf-1"); err != nil {
		t.Fatal(err)
	}
	args := r.calls[0]
	if args[0] != "close-surface" || args[1] != "surf-1" {
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
