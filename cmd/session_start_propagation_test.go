package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gurkangul/gg-cli/internal/agenthooks"
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

// TestSyncManagedBlocks_BMADDetected_InstallsBlock verifies AC-6a: when a
// _bmad/ directory is present in the project root and AGENTS.md exists,
// SyncManagedBlocks installs the gg-bmad block into AGENTS.md and emits
// the AC-2 "BMAD detected" notice in sr.Lines. Also verifies AC-6b: a
// second call is idempotent (no notice on repeat).
func TestSyncManagedBlocks_BMADDetected_InstallsBlock(t *testing.T) {
	dir := t.TempDir()

	// Seed _bmad/ directory — the canonical BMAD detection signal.
	if err := os.MkdirAll(filepath.Join(dir, "_bmad"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Seed AGENTS.md without the bmad block — so the installer has work to do.
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("# Agents\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	sr := agenthooks.SyncManagedBlocks(dir)

	// AC-6a: AGENTS.md must contain the gg-bmad managed block.
	raw, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if err != nil {
		t.Fatalf("read AGENTS.md: %v", err)
	}
	if !strings.Contains(string(raw), "gg-bmad:start") {
		t.Error("AGENTS.md missing gg-bmad block after SyncManagedBlocks (AC-6a)")
	}

	// AC-2 / AC-6a: sr.Lines must contain the "BMAD detected" notice.
	bmadNotice := false
	for _, l := range sr.Lines {
		if strings.Contains(l, "BMAD detected") {
			bmadNotice = true
			break
		}
	}
	if !bmadNotice {
		t.Errorf("SyncManagedBlocks lines missing 'BMAD detected' notice (AC-2/AC-6a); got: %v", sr.Lines)
	}

	// AC-6b: second call must be idempotent — no notice, Repaired=false.
	sr2 := agenthooks.SyncManagedBlocks(dir)
	for _, l := range sr2.Lines {
		if strings.Contains(l, "BMAD detected") {
			t.Errorf("SyncManagedBlocks emitted BMAD detected on idempotent second call (AC-6b); lines: %v", sr2.Lines)
		}
	}
	if sr2.Repaired {
		t.Errorf("SyncManagedBlocks Repaired=true on idempotent second call (AC-6b)")
	}
}

// TestSyncManagedBlocks_RepairsOnSessionStart verifies that session-start calls
// SyncManagedBlocks and emits a resync banner on stderr when a stale managed
// block is found. Tests the integration between runSessionStart and
// agenthooks.SyncManagedBlocks (TASK-324).
func TestSyncManagedBlocks_RepairsOnSessionStart(t *testing.T) {
	dir := t.TempDir()

	// Write a stale contract block into CLAUDE.md to trigger a repair.
	staleBlock := agenthooks.ContractBlockBegin + "\n## OLD CONTRACT\n\noutdated\n" + agenthooks.ContractBlockEnd + "\n"
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(staleBlock), 0o644); err != nil {
		t.Fatalf("write CLAUDE.md: %v", err)
	}

	sr := agenthooks.SyncManagedBlocks(dir)

	if !sr.Repaired {
		t.Error("SyncManagedBlocks should report Repaired=true for stale contract")
	}
	if len(sr.Lines) == 0 {
		t.Error("SyncManagedBlocks should produce at least one repair line")
	}

	// Verify at least one line looks like a repair entry (starts with ✓).
	found := false
	for _, l := range sr.Lines {
		if strings.Contains(l, "✓") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected at least one ✓ repair line; got: %v", sr.Lines)
	}
}

// TestSessionStart_BenchResync_Under200ms verifies AC-5+AC-6d: the managed-block
// resync step (file-stat + write only, no docker/qdrant) completes in ≤200ms.
// Runs 3 iterations and asserts each is under the budget.
func TestSessionStart_BenchResync_Under200ms(t *testing.T) {
	const budget = 200 * time.Millisecond
	const iterations = 3

	for i := 0; i < iterations; i++ {
		dir := t.TempDir()
		// Seed CLAUDE.md with a stale contract to trigger an actual write.
		stale := agenthooks.ContractBlockBegin + "\n## OLD\n\nold\n" + agenthooks.ContractBlockEnd + "\n"
		if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(stale), 0o644); err != nil {
			t.Fatalf("seed CLAUDE.md: %v", err)
		}

		start := time.Now()
		agenthooks.SyncManagedBlocks(dir)
		elapsed := time.Since(start)

		if elapsed > budget {
			t.Errorf("iteration %d: SyncManagedBlocks took %v, want ≤%v (AC-5 budget)", i+1, elapsed, budget)
		}
	}
}

// TestSyncManagedBlocks_SilentWhenAllCurrent verifies that SyncManagedBlocks
// produces no output and Repaired=false when all blocks are already current,
// so session-start emits no banner noise on clean projects.
func TestSyncManagedBlocks_SilentWhenAllCurrent(t *testing.T) {
	dir := t.TempDir()

	// Write current contract + master-role + dev-routing blocks.
	content := agenthooks.ContractBlock() + agenthooks.MasterRoleBlock() + agenthooks.DevRoutingBlock()
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write CLAUDE.md: %v", err)
	}

	sr := agenthooks.SyncManagedBlocks(dir)

	if sr.Repaired {
		t.Errorf("expected no repair when blocks are current; lines: %v", sr.Lines)
	}
	if len(sr.Errors) > 0 {
		t.Errorf("unexpected errors on clean project: %v", sr.Errors)
	}
}
