package terminal

import (
	"context"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"
)

// agentBusyExact are substrings matched anywhere in the tail content (case-insensitive).
// These are precise enough that a false positive is extremely unlikely.
var agentBusyExact = []string{
	// Claude Code spinner runes (Braille patterns used by the TUI)
	"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏",
	// Claude Code active-turn labels
	"thinking…", "thinking...",
	// GSD / pi active labels
	"working…", "working...",
	// Generic agent busy signals
	"tool call", "tool_call",
}

// agentBusyWordBounded are matched only at word boundaries so that common
// English words embedded in paths, identifiers, or log lines are not false
// positives (e.g. "outrunning", "test_executing.sh", "task-running").
var agentBusyWordBounded = []*regexp.Regexp{
	// "running" / "executing" as whole words (case-insensitive via (?i))
	regexp.MustCompile(`(?i)\brunning\b`),
	regexp.MustCompile(`(?i)\bexecuting\b`),
}

// IsAgentIdle reports whether the screen content suggests the embedded agent
// REPL has finished its current turn and is waiting for a new user message.
//
// The heuristic inspects the last five non-blank lines: if any busy marker is
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

	// Exact substring matches (precise markers).
	for _, marker := range agentBusyExact {
		if strings.Contains(lower, strings.ToLower(marker)) {
			return false
		}
	}

	// Word-bounded matches (generic markers that need anchoring).
	combined := strings.Join(tail, "\n")
	for _, re := range agentBusyWordBounded {
		if re.MatchString(combined) {
			return false
		}
	}

	return true
}

// wakeDelay is the pause between the wake keypress and the real payload send.
// Long enough for the REPL stdin handler to register the Enter, short enough
// to be invisible in the normal flow.
const wakeDelay = 150 * time.Millisecond

// surfaceLocks provides per-surface mutual exclusion for WakeAndSend so that
// concurrent callers for the same surface cannot interleave the wake-Enter and
// the text payload.
var surfaceLocks = &lockMap{}

type lockMap struct {
	mu    sync.Mutex
	locks map[SurfaceID]*sync.Mutex
}

func (m *lockMap) get(id SurfaceID) *sync.Mutex {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.locks == nil {
		m.locks = make(map[SurfaceID]*sync.Mutex)
	}
	if m.locks[id] == nil {
		m.locks[id] = &sync.Mutex{}
	}
	return m.locks[id]
}

// WakeAndSend ensures a new agent turn is started in the target pane and then
// delivers text.  When the pane appears idle (turn complete), it sends a bare
// Enter first to wake the REPL's stdin handler before queuing the real prompt.
// When the pane is mid-turn the prompt is delivered directly and lands in the
// Steering queue as usual — no observable difference for the running agent.
//
// A follow-up Enter is always sent after text so the agent processes the message.
// Returns the first error encountered; subsequent operations are skipped.
//
// WakeAndSend serialises concurrent callers per surface: the lock prevents a
// second caller from interleaving its wake-Enter with the first caller's text
// send, which would corrupt the agent's stdin.
func WakeAndSend(ctx context.Context, term Terminal, id SurfaceID, text string) error {
	// Acquire the per-surface lock before touching the terminal.
	lock := surfaceLocks.get(id)
	lock.Lock()
	defer lock.Unlock()

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
