package terminal

// Integration tests for real cmux and tmux backends.
//
// These tests shell out to the actual binaries and require a running daemon /
// active session.  They are skipped unless GG_INTEGRATION_TEST=1 is set.
//
// Usage:
//
//	GG_INTEGRATION_TEST=1 go test ./internal/orchestrator/terminal/ \
//	    -run TestIntegration -v -count=1 -timeout 60s
//
// Pre-conditions:
//   - cmux: the cmux daemon must be running (cmux ping responds with PONG).
//   - tmux: a tmux server must be reachable and able to create new sessions
//     (the test creates a detached session and kills it on cleanup).

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// skipUnlessIntegration skips the test when the integration gate is not set.
func skipUnlessIntegration(t *testing.T) {
	t.Helper()
	if os.Getenv("GG_INTEGRATION_TEST") != "1" {
		t.Skip("set GG_INTEGRATION_TEST=1 to run terminal integration tests")
	}
}

// ---------------------------------------------------------------------------
// cmux integration
// ---------------------------------------------------------------------------

// cmuxAvailable returns true if the cmux binary is in PATH and the daemon
// responds to a ping.
func cmuxAvailable() bool {
	if _, err := exec.LookPath("cmux"); err != nil {
		return false
	}
	cmd := exec.Command("cmux", "ping")
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "PONG"
}

// TestIntegration_Cmux_NewSplitSendClose creates a real cmux pane, sends a
// line into it, reads back the screen, then closes the pane.
func TestIntegration_Cmux_NewSplitSendClose(t *testing.T) {
	skipUnlessIntegration(t)
	if !cmuxAvailable() {
		t.Skip("cmux daemon not reachable (run cmux first)")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	term := newCmux()

	id, err := term.NewSplit(ctx, SplitOpts{Dir: SplitHorizontal, Percent: 30})
	if err != nil {
		t.Fatalf("NewSplit: %v", err)
	}
	t.Logf("cmux surface created: %s", id)
	t.Cleanup(func() {
		// Best-effort close; already closed by the test on the happy path.
		_ = term.Close(context.Background(), id)
	})

	if err := term.Send(ctx, id, "echo gg-inttest-cmux"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if err := term.SendKey(ctx, id, "Enter"); err != nil {
		t.Fatalf("SendKey: %v", err)
	}

	// Give the shell a moment to echo the line before we capture the screen.
	time.Sleep(300 * time.Millisecond)

	screen, err := term.ReadScreen(ctx, id)
	if err != nil {
		t.Fatalf("ReadScreen: %v", err)
	}
	t.Logf("cmux screen:\n%s", screen)
	if !bytes.Contains(screen, []byte("gg-inttest-cmux")) {
		t.Errorf("expected 'gg-inttest-cmux' in screen output; got:\n%s", screen)
	}

	if err := term.Close(ctx, id); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// TestIntegration_Cmux_Capabilities asserts the real cmux backend reports
// CanReadScreen=true.
func TestIntegration_Cmux_Capabilities(t *testing.T) {
	skipUnlessIntegration(t)
	if !cmuxAvailable() {
		t.Skip("cmux daemon not reachable")
	}

	term := newCmux()
	if !term.Capabilities().CanReadScreen {
		t.Fatal("cmux backend must advertise CanReadScreen=true")
	}
}

// ---------------------------------------------------------------------------
// tmux integration
// ---------------------------------------------------------------------------

// tmuxAvailable returns true if the tmux binary is in PATH.
func tmuxAvailable() bool {
	_, err := exec.LookPath("tmux")
	return err == nil
}

// newTmuxTestSession creates a detached tmux session for testing and returns
// the session name.  The caller must run cleanupTmuxSession when done.
func newTmuxTestSession(t *testing.T) string {
	t.Helper()
	name := "gg-inttest-" + t.Name()
	// Replace characters that tmux doesn't allow in session names.
	name = strings.NewReplacer("/", "-", " ", "-").Replace(name)
	cmd := exec.Command("tmux", "new-session", "-d", "-s", name)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("cannot create tmux test session %q: %v\n%s", name, err, out)
	}
	return name
}

func cleanupTmuxSession(name string) {
	_ = exec.Command("tmux", "kill-session", "-t", name).Run()
}

// TestIntegration_Tmux_NewSplitSendClose creates a split inside a temporary
// tmux session, sends a line, reads back the pane content, then closes the pane.
func TestIntegration_Tmux_NewSplitSendClose(t *testing.T) {
	skipUnlessIntegration(t)
	if !tmuxAvailable() {
		t.Skip("tmux not in PATH")
	}

	session := newTmuxTestSession(t)
	t.Cleanup(func() { cleanupTmuxSession(session) })

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Point the runner at the session so splits land in our test session.
	// tmux split-window inside a named session requires -t <session>.
	// We override the runner to inject "-t <session>" before the command args.
	term := newTmuxWithRunner(func(ctx context.Context, args ...string) ([]byte, error) {
		// Prepend -t session for commands that operate on panes.
		// Commands that already carry a pane ID ("-t %N") are passed through.
		enriched := injectTmuxSession(session, args)
		cmd := exec.CommandContext(ctx, "tmux", enriched...)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		if err != nil {
			msg := strings.TrimSpace(stderr.String())
			if msg == "" {
				msg = err.Error()
			}
			return nil, fmt.Errorf("tmux %v: %s: %w", enriched, msg, err)
		}
		return out, nil
	})

	id, err := term.NewSplit(ctx, SplitOpts{Dir: SplitHorizontal, Percent: 30})
	if err != nil {
		t.Fatalf("NewSplit: %v", err)
	}
	t.Logf("tmux pane created: %s", id)
	t.Cleanup(func() {
		_ = term.Close(context.Background(), id)
	})

	if err := term.Send(ctx, id, "echo gg-inttest-tmux"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if err := term.SendKey(ctx, id, "Enter"); err != nil {
		t.Fatalf("SendKey: %v", err)
	}

	time.Sleep(300 * time.Millisecond)

	screen, err := term.ReadScreen(ctx, id)
	if err != nil {
		t.Fatalf("ReadScreen: %v", err)
	}
	t.Logf("tmux screen:\n%s", screen)
	if !bytes.Contains(screen, []byte("gg-inttest-tmux")) {
		t.Errorf("expected 'gg-inttest-tmux' in screen; got:\n%s", screen)
	}

	if err := term.Close(ctx, id); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// TestIntegration_Tmux_Capabilities asserts the real tmux backend reports
// CanReadScreen=true.
func TestIntegration_Tmux_Capabilities(t *testing.T) {
	skipUnlessIntegration(t)
	if !tmuxAvailable() {
		t.Skip("tmux not in PATH")
	}

	term := newTmux()
	if !term.Capabilities().CanReadScreen {
		t.Fatal("tmux backend must advertise CanReadScreen=true")
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// injectTmuxSession prepends -t <session> to split-window commands so they
// target the test session, and passes other commands unchanged (they already
// carry a pane ref via -t %N).
func injectTmuxSession(session string, args []string) []string {
	if len(args) == 0 {
		return args
	}
	switch args[0] {
	case "split-window":
		// Inject -t <session> right after the subcommand.
		result := make([]string, 0, len(args)+2)
		result = append(result, args[0], "-t", session)
		result = append(result, args[1:]...)
		return result
	default:
		return args
	}
}
