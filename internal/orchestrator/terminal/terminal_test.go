package terminal_test

import (
	"context"
	"testing"

	"github.com/gurkangul/gg-cli/internal/orchestrator/terminal"
)

func TestFakeTerminal_NewSplit(t *testing.T) {
	f := terminal.NewFake()
	ctx := context.Background()

	id, err := f.NewSplit(ctx, terminal.SplitOpts{Dir: terminal.SplitHorizontal, Cmd: "bash"})
	if err != nil {
		t.Fatal(err)
	}
	if id == "" {
		t.Fatal("expected non-empty SurfaceID")
	}
	if !f.IsAlive(id) {
		t.Fatal("surface should be alive after NewSplit")
	}
	if len(f.Calls) != 1 || f.Calls[0].Method != "NewSplit" {
		t.Fatalf("unexpected Calls: %v", f.Calls)
	}
}

func TestFakeTerminal_SendAndRead(t *testing.T) {
	f := terminal.NewFake()
	ctx := context.Background()

	id, _ := f.NewSplit(ctx, terminal.SplitOpts{})
	if err := f.Send(ctx, id, "hello"); err != nil {
		t.Fatal(err)
	}
	got, err := f.ReadScreen(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello" {
		t.Fatalf("want %q got %q", "hello", string(got))
	}
}

func TestFakeTerminal_SendKey(t *testing.T) {
	f := terminal.NewFake()
	ctx := context.Background()

	id, _ := f.NewSplit(ctx, terminal.SplitOpts{})
	if err := f.SendKey(ctx, id, "Enter"); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range f.Calls {
		if c.Method == "SendKey" && c.Arg == "Enter" {
			found = true
		}
	}
	if !found {
		t.Fatalf("SendKey call not recorded; calls=%v", f.Calls)
	}
}

func TestFakeTerminal_Focus(t *testing.T) {
	f := terminal.NewFake()
	ctx := context.Background()

	id, _ := f.NewSplit(ctx, terminal.SplitOpts{})
	if err := f.Focus(ctx, id); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range f.Calls {
		if c.Method == "Focus" {
			found = true
		}
	}
	if !found {
		t.Fatal("Focus call not recorded")
	}
}

func TestFakeTerminal_Close(t *testing.T) {
	f := terminal.NewFake()
	ctx := context.Background()

	id, _ := f.NewSplit(ctx, terminal.SplitOpts{})
	if err := f.Close(ctx, id); err != nil {
		t.Fatal(err)
	}
	if f.IsAlive(id) {
		t.Fatal("surface should not be alive after Close")
	}
}

func TestFakeTerminal_ErrorsAfterClose(t *testing.T) {
	f := terminal.NewFake()
	ctx := context.Background()

	id, _ := f.NewSplit(ctx, terminal.SplitOpts{})
	_ = f.Close(ctx, id)

	if err := f.Send(ctx, id, "x"); err == nil {
		t.Fatal("expected error sending to closed surface")
	}
	if _, err := f.ReadScreen(ctx, id); err == nil {
		t.Fatal("expected error reading closed surface")
	}
}

func TestFakeTerminal_Capabilities(t *testing.T) {
	f := terminal.NewFake()
	if !f.Capabilities().CanReadScreen {
		t.Fatal("fake terminal must advertise CanReadScreen=true")
	}
}

func TestFakeTerminal_SetScreen(t *testing.T) {
	f := terminal.NewFake()
	ctx := context.Background()

	id, _ := f.NewSplit(ctx, terminal.SplitOpts{})
	f.SetScreen(id, []byte("custom content"))
	got, _ := f.ReadScreen(ctx, id)
	if string(got) != "custom content" {
		t.Fatalf("want %q got %q", "custom content", string(got))
	}
}

func TestFakeTerminal_CallOrder(t *testing.T) {
	f := terminal.NewFake()
	ctx := context.Background()

	id, _ := f.NewSplit(ctx, terminal.SplitOpts{})
	_ = f.Send(ctx, id, "a")
	_ = f.SendKey(ctx, id, "Enter")
	_, _ = f.ReadScreen(ctx, id)
	_ = f.Focus(ctx, id)
	_ = f.Close(ctx, id)

	want := []string{"NewSplit", "Send", "SendKey", "ReadScreen", "Focus", "Close"}
	if len(f.Calls) != len(want) {
		t.Fatalf("want %d calls got %d: %v", len(want), len(f.Calls), f.Calls)
	}
	for i, m := range want {
		if f.Calls[i].Method != m {
			t.Errorf("call[%d]: want %q got %q", i, m, f.Calls[i].Method)
		}
	}
}

func TestNew_FakeKind(t *testing.T) {
	term, err := terminal.New("fake")
	if err != nil {
		t.Fatal(err)
	}
	if term == nil {
		t.Fatal("expected non-nil terminal")
	}
}

func TestNew_UnknownKind(t *testing.T) {
	_, err := terminal.New("bogus")
	if err == nil {
		t.Fatal("expected error for unknown kind")
	}
}

func TestNew_TmuxNotImplemented(t *testing.T) {
	_, err := terminal.New("tmux")
	if err == nil {
		t.Fatal("expected error for tmux (not yet implemented)")
	}
}
