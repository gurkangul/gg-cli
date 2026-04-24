package terminal

import (
	"context"
	"strings"
	"time"
	"unicode"
)

// agentBusyMarkers are substrings that appear in the visible terminal content
// while a known agent REPL (claude-code, GSD/pi) is mid-turn.  Their presence
// means the pane is processing; their absence suggests the turn is complete and
// the REPL is waiting for the next user message.
//
// Markers are matched case-insensitively against the last non-blank line(s) of
// the screen capture.
var agentBusyMarkers = []string{
	// Claude Code spinner runes (Braille patterns used by the TUI)
	"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏",
	// Claude Code active-turn labels
	"thinking…", "thinking...",
	// GSD / pi active labels
	"working…", "working...",
	"running", "executing",
	// Generic agent busy signals
	"tool call", "tool_call",
}

// IsAgentIdle reports whether the screen content suggests the embedded agent
// REPL has finished its current turn and is waiting for a new user message.
//
// The heuristic inspects the last few non-blank lines: if any busy marker is
// present the pane is considered active.  An empty screen is treated as idle
// (no agent started, or cleared).
func IsAgentIdle(screen []byte) bool {
	if len(screen) == 0 {
		return true
	}
	// Collect the last 5 non-blank lines for the scan.
	lines := strings.Split(string(screen), "\n")
	tail := make([]string, 0, 5)
	for i := len(lines) - 1; i >= 0 && len(tail) < 5; i-- {
		if trimmed := strings.TrimSpace(lines[i]); trimmed != "" {
			tail = append(tail, trimmed)
		}
	}
	lower := strings.ToLower(strings.Join(tail, "\n"))
	for _, marker := range agentBusyMarkers {
		if strings.Contains(lower, strings.ToLower(marker)) {
			return false
		}
	}
	return true
}

// wakeDelay is the pause between the wake keypress and the real payload send.
// Long enough for the REPL stdin handler to register the Enter, short enough
// to be invisible in the normal flow.
const wakeDelay = 150 * time.Millisecond

// WakeAndSend ensures a new agent turn is started in the target pane and then
// delivers text.  When the pane appears idle (turn complete), it sends a bare
// Enter first to wake the REPL's stdin handler before queuing the real prompt.
// When the pane is mid-turn the prompt is delivered directly and lands in the
// Steering queue as usual — no observable difference for the running agent.
//
// A follow-up Enter is always sent after text so the agent processes the message.
// Returns the first error encountered; subsequent operations are skipped.
func WakeAndSend(ctx context.Context, term Terminal, id SurfaceID, text string) error {
	// Read screen to determine pane state.  Failures are treated as "unknown /
	// possibly idle" — we issue the wake sequence on the safe side.
	idle := true
	if term.Capabilities().CanReadScreen {
		if content, err := term.ReadScreen(ctx, id); err == nil {
			idle = IsAgentIdle(content)
		}
	}

	if idle {
		// Wake the REPL by sending a bare Enter.  This is a no-op when the REPL
		// is already waiting: it echoes an empty line but does not submit any
		// message.  Without this, text written to an idle REPL's input buffer
		// does not trigger a new turn.
		if err := term.SendKey(ctx, id, "enter"); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wakeDelay):
		}
	}

	if err := term.Send(ctx, id, text); err != nil {
		return err
	}
	return term.SendKey(ctx, id, "enter")
}

// lastNonBlankLine returns the last line in s that is not all whitespace,
// or empty string if none exists.  Used in tests.
func lastNonBlankLine(s string) string {
	lines := strings.Split(s, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.IndexFunc(lines[i], func(r rune) bool { return !unicode.IsSpace(r) }) >= 0 {
			return lines[i]
		}
	}
	return ""
}
