package terminal

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestTmux_NewSplit_Vertical(t *testing.T) {
	r := newCapture("%3\n")
	tm := newTmuxWithRunner(r.run)
	ctx := context.Background()

	id, err := tm.NewSplit(ctx, SplitOpts{Dir: SplitVertical})
	if err != nil {
		t.Fatal(err)
	}
	if id != "%3" {
		t.Fatalf("want %%3 got %q", id)
	}
	args := r.calls[0]
	if args[0] != "split-window" {
		t.Fatalf("expected split-window, got %q", args[0])
	}
	found := false
	for _, a := range args {
		if a == "-h" {
			found = true
		}
	}
	if !found {
		t.Fatalf("-h flag missing for SplitVertical: %v", args)
	}
}

func TestTmux_NewSplit_Horizontal(t *testing.T) {
	r := newCapture("%5\n")
	tm := newTmuxWithRunner(r.run)
	ctx := context.Background()

	id, err := tm.NewSplit(ctx, SplitOpts{Dir: SplitHorizontal})
	if err != nil {
		t.Fatal(err)
	}
	if id != "%5" {
		t.Fatalf("want %%5 got %q", id)
	}
	args := r.calls[0]
	for _, a := range args {
		if a == "-h" {
			t.Fatalf("-h flag must NOT appear for SplitHorizontal: %v", args)
		}
	}
}

func TestTmux_NewSplit_Percent(t *testing.T) {
	r := newCapture("%7\n")
	tm := newTmuxWithRunner(r.run)
	ctx := context.Background()

	_, err := tm.NewSplit(ctx, SplitOpts{Percent: 40})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, a := range r.calls[0] {
		if a == "-p40" {
			found = true
		}
	}
	if !found {
		t.Fatalf("-p40 missing from args: %v", r.calls[0])
	}
}

func TestTmux_NewSplit_Env(t *testing.T) {
	r := newCapture("%8\n")
	tm := newTmuxWithRunner(r.run)
	ctx := context.Background()

	_, err := tm.NewSplit(ctx, SplitOpts{Env: []string{"FOO=bar", "BAZ=qux"}})
	if err != nil {
		t.Fatal(err)
	}
	args := r.calls[0]
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "FOO=bar") || !strings.Contains(joined, "BAZ=qux") {
		t.Fatalf("env vars missing from args: %v", args)
	}
}

func TestTmux_NewSplit_WithCmd(t *testing.T) {
	r := newCapture("%9\n")
	tm := newTmuxWithRunner(r.run)
	ctx := context.Background()

	_, err := tm.NewSplit(ctx, SplitOpts{Cmd: "bash"})
	if err != nil {
		t.Fatal(err)
	}
	args := r.calls[0]
	last := args[len(args)-1]
	if last != "bash" {
		t.Fatalf("command should be last arg, got %q in %v", last, args)
	}
}

func TestTmux_NewSplit_EmptyOutput(t *testing.T) {
	r := newCapture("   \n")
	tm := newTmuxWithRunner(r.run)
	ctx := context.Background()

	_, err := tm.NewSplit(ctx, SplitOpts{})
	if err == nil {
		t.Fatal("expected error for empty pane ID")
	}
	if !strings.Contains(err.Error(), "empty pane ID") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTmux_NewSplit_RunnerError(t *testing.T) {
	r := &captureRunner{err: errors.New("tmux not found")}
	tm := newTmuxWithRunner(r.run)
	ctx := context.Background()

	_, err := tm.NewSplit(ctx, SplitOpts{})
	if err == nil {
		t.Fatal("expected error when runner fails")
	}
}

func TestTmux_Send(t *testing.T) {
	r := newCapture("")
	tm := newTmuxWithRunner(r.run)
	ctx := context.Background()

	if err := tm.Send(ctx, "%3", "hello world"); err != nil {
		t.Fatal(err)
	}
	args := r.calls[0]
	// Expected: send-keys -l -t %3 <text>
	if args[0] != "send-keys" || args[1] != "-l" || args[2] != "-t" || args[3] != "%3" || args[4] != "hello world" {
		t.Fatalf("unexpected args: %v", args)
	}
	if len(args) != 5 {
		t.Fatalf("expected exactly 5 args, got %d: %v", len(args), args)
	}
}

func TestTmux_SendKey(t *testing.T) {
	r := newCapture("")
	tm := newTmuxWithRunner(r.run)
	ctx := context.Background()

	if err := tm.SendKey(ctx, "%3", "Enter"); err != nil {
		t.Fatal(err)
	}
	args := r.calls[0]
	if args[0] != "send-keys" || args[1] != "-t" || args[2] != "%3" || args[3] != "Enter" {
		t.Fatalf("unexpected args: %v", args)
	}
}

func TestTmux_ReadScreen(t *testing.T) {
	r := newCapture("pane content here")
	tm := newTmuxWithRunner(r.run)
	ctx := context.Background()

	out, err := tm.ReadScreen(ctx, "%3")
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "pane content here" {
		t.Fatalf("want %q got %q", "pane content here", string(out))
	}
	args := r.calls[0]
	if args[0] != "capture-pane" || args[1] != "-p" || args[2] != "-t" || args[3] != "%3" {
		t.Fatalf("unexpected args: %v", args)
	}
}

func TestTmux_Focus(t *testing.T) {
	r := newCapture("")
	tm := newTmuxWithRunner(r.run)
	ctx := context.Background()

	if err := tm.Focus(ctx, "%3"); err != nil {
		t.Fatal(err)
	}
	args := r.calls[0]
	if args[0] != "select-pane" || args[1] != "-t" || args[2] != "%3" {
		t.Fatalf("unexpected args: %v", args)
	}
}

func TestTmux_Close(t *testing.T) {
	r := newCapture("")
	tm := newTmuxWithRunner(r.run)
	ctx := context.Background()

	if err := tm.Close(ctx, "%3"); err != nil {
		t.Fatal(err)
	}
	args := r.calls[0]
	if args[0] != "kill-pane" || args[1] != "-t" || args[2] != "%3" {
		t.Fatalf("unexpected args: %v", args)
	}
}

func TestTmux_Capabilities(t *testing.T) {
	tm := newTmux()
	if !tm.Capabilities().CanReadScreen {
		t.Fatal("tmux must advertise CanReadScreen=true")
	}
}

func TestTmux_execRun_ErrorFormat(t *testing.T) {
	tm := newTmux()
	ctx := context.Background()

	tm.runner = func(_ context.Context, args ...string) ([]byte, error) {
		return nil, errors.New("tmux split-window: can't find window")
	}

	_, err := tm.NewSplit(ctx, SplitOpts{})
	if err == nil {
		t.Fatal("expected error")
	}
}
