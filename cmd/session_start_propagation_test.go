package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/gurkangul/gg-cli/internal/changelog"
	"github.com/gurkangul/gg-cli/internal/projectstate"
)

// TestSessionStart_LastSeenDelta_E2E proves that emitVersionDelta's body
// (writeVersionDelta) actually fires the VERSION UPDATE notice when the
// persisted LSCV differs from the current version. Closes the gap Amelia
// flagged in the TASK-236→241 dogfood retro (2026-04-20): the cluster
// shipped with 15 unit tests but zero test proved the propagation path
// end-to-end in a real state.json.
func TestSessionStart_LastSeenDelta_E2E(t *testing.T) {
	dir := t.TempDir()

	// Seed state.json with a stale LSCV + a BypassEntry so we can also verify
	// the session-start bug-fix (LSCV write must not clobber BypassLog).
	seed := projectstate.State{
		LastSeenCLIVersion: "v0.1.0",
		BypassLog: []projectstate.BypassEntry{
			{TS: "2026-04-19T12:00:00Z", Gate: "pre-task-done", TaskID: "TASK-207"},
		},
	}
	if err := projectstate.Write(dir, seed); err != nil {
		t.Fatalf("seed state: %v", err)
	}

	// Feed a minimal CHANGELOG so Since() returns a real excerpt — otherwise
	// the code falls back to the header-only branch and we wouldn't test the
	// truncation path.
	changelog.SetContent("# CHANGELOG\n\n## v0.2.0\n- second\n\n## v0.1.0\n- first\n")
	t.Cleanup(func() { changelog.SetContent("") })

	var buf bytes.Buffer
	writeVersionDelta(&buf, dir, "v0.2.0")

	out := buf.String()
	if !strings.Contains(out, "─── VERSION UPDATE: v0.1.0 → v0.2.0 ───") {
		t.Fatalf("expected VERSION UPDATE header, got:\n%s", out)
	}

	// LSCV must now equal the "current" version — next session will be silent.
	after, err := projectstate.Read(dir)
	if err != nil {
		t.Fatalf("read after: %v", err)
	}
	if after.LastSeenCLIVersion != "v0.2.0" {
		t.Fatalf("LSCV not updated: got %q, want v0.2.0", after.LastSeenCLIVersion)
	}

	// BypassLog must survive — regression guard for the clobber bug fixed
	// alongside TASK-243.
	if len(after.BypassLog) != 1 || after.BypassLog[0].TaskID != "TASK-207" {
		t.Fatalf("BypassLog clobbered by session-start: %+v", after.BypassLog)
	}
}

// TestSessionStart_NoDelta_WhenVersionsMatch keeps session-start quiet when
// the consumer is already on the current CLI. Otherwise every session emits
// a noisy banner and agents learn to ignore it.
func TestSessionStart_NoDelta_WhenVersionsMatch(t *testing.T) {
	dir := t.TempDir()
	if err := projectstate.Write(dir, projectstate.State{LastSeenCLIVersion: "v0.2.0"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	changelog.SetContent("# CHANGELOG\n\n## v0.2.0\n- release\n")
	t.Cleanup(func() { changelog.SetContent("") })

	var buf bytes.Buffer
	writeVersionDelta(&buf, dir, "v0.2.0")
	if buf.Len() != 0 {
		t.Fatalf("expected silent when versions match, got:\n%s", buf.String())
	}
}

// TestSessionStart_FirstSession_NoDelta validates the "never seen" case:
// empty LSCV must not produce a VERSION UPDATE block (nothing to compare
// against) but must still stamp the current version so future runs fire.
func TestSessionStart_FirstSession_NoDelta(t *testing.T) {
	dir := t.TempDir() // no state.json present

	var buf bytes.Buffer
	writeVersionDelta(&buf, dir, "v0.3.0")
	if strings.Contains(buf.String(), "VERSION UPDATE") {
		t.Fatalf("first session should be silent, got:\n%s", buf.String())
	}

	after, err := projectstate.Read(dir)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if after.LastSeenCLIVersion != "v0.3.0" {
		t.Fatalf("LSCV must be stamped on first session: got %q", after.LastSeenCLIVersion)
	}
}
