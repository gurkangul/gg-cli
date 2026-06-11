package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestMaybeInstallIndexHooks_Installs verifies that the init helper (AC-1)
// installs all three CodeGraph hooks into a fresh repo and reports installed.
func TestMaybeInstallIndexHooks_Installs(t *testing.T) {
	root := t.TempDir()
	if out, err := exec.Command("git", "-C", root, "init").CombinedOutput(); err != nil {
		t.Skipf("git init: %v\n%s", err, out)
	}
	resetIndexHookFlags(t)
	if !maybeInstallIndexHooks(root) {
		t.Fatalf("maybeInstallIndexHooks returned false; want installed")
	}
	if !indexHooksInstalled(root) {
		t.Errorf("hooks not present after maybeInstallIndexHooks")
	}
}

// TestMaybeInstallIndexHooks_FlagOptOut verifies --no-index-hooks skips install
// and the hooks stay absent (AC-1 opt-out).
func TestMaybeInstallIndexHooks_FlagOptOut(t *testing.T) {
	root := t.TempDir()
	if out, err := exec.Command("git", "-C", root, "init").CombinedOutput(); err != nil {
		t.Skipf("git init: %v\n%s", err, out)
	}
	resetIndexHookFlags(t)
	initNoIndexHooks = true
	if maybeInstallIndexHooks(root) {
		t.Fatalf("maybeInstallIndexHooks installed despite --no-index-hooks")
	}
	if indexHooksInstalled(root) {
		t.Errorf("hooks installed despite opt-out")
	}
}

// TestIndexHooksOptOut_Env verifies GG_NO_INDEX_HOOKS opts out.
func TestIndexHooksOptOut_Env(t *testing.T) {
	resetIndexHookFlags(t)
	t.Setenv("GG_NO_INDEX_HOOKS", "1")
	if !indexHooksOptOut() {
		t.Errorf("GG_NO_INDEX_HOOKS=1 did not opt out")
	}
}

// TestIndexHooksOptOut_ConfigKey verifies auto_index_hooks: false in the
// project config opts out.
func TestIndexHooksOptOut_ConfigKey(t *testing.T) {
	resetIndexHookFlags(t)
	root := t.TempDir()
	ggDir := filepath.Join(root, ".gg")
	if err := os.MkdirAll(ggDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := "schema_version: 1\n" +
		"project_id: 11111111-1111-1111-1111-111111111111\n" +
		"qdrant:\n  host: localhost\n  port: 6334\n" +
		"embedding:\n  host: http://localhost:11434\n  model: nomic-embed-text\n" +
		"memgraph:\n  uri: bolt://localhost:7687\n" +
		"backup:\n  enabled: true\n  interval: 24h\n  timeout: 30s\n" +
		"auto_index_hooks: false\n"
	if err := os.WriteFile(filepath.Join(ggDir, "config.yaml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	if !indexHooksOptOut() {
		t.Errorf("auto_index_hooks: false did not opt out")
	}
}

// TestRenderCodeGraphStatusCompact_HooksToken verifies status surfaces the
// index-hook install state (AC-3): hooks=off + install prompt when the graph is
// stale and hooks are missing.
func TestRenderCodeGraphStatusCompact_HooksToken(t *testing.T) {
	root := t.TempDir()
	if out, err := exec.Command("git", "-C", root, "init").CombinedOutput(); err != nil {
		t.Skipf("git init: %v\n%s", err, out)
	}
	ggDir := filepath.Join(root, ".gg")
	if err := os.MkdirAll(ggDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := "schema_version: 1\nproject_id: 33333333-3333-3333-3333-333333333333\n" +
		"qdrant:\n  host: localhost\n  port: 6334\n" +
		"embedding:\n  host: http://localhost:11434\n  model: nomic-embed-text\n" +
		"memgraph:\n  uri: bolt://localhost:7687\n" +
		"backup:\n  enabled: true\n  interval: 24h\n  timeout: 30s\n"
	if err := os.WriteFile(filepath.Join(ggDir, "config.yaml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)

	// Hooks missing + a stale status → hooks=off and the install prompt.
	out := renderCodeGraphStatusCompact(codeGraphStatus{Status: "stale", ChangedFiles: 1, DetectedLanguages: []string{"go"}})
	if !strings.Contains(out, "hooks=off") {
		t.Errorf("missing hooks=off in %q", out)
	}
	if !strings.Contains(out, "install-hooks=gg doctor --install-index-hooks") {
		t.Errorf("missing install prompt in %q", out)
	}

	// After install → hooks=on and no prompt.
	if err := installGitIndexHooks(root); err != nil {
		t.Fatal(err)
	}
	out = renderCodeGraphStatusCompact(codeGraphStatus{Status: "stale", ChangedFiles: 1, DetectedLanguages: []string{"go"}})
	if !strings.Contains(out, "hooks=on") {
		t.Errorf("missing hooks=on after install in %q", out)
	}
	if strings.Contains(out, "install-hooks=") {
		t.Errorf("install prompt still shown after install: %q", out)
	}
}

// TestDoctorCheckIndexHooks verifies the doctor surfacing (AC-3): fail when
// stale+missing (problems incremented), warn when fresh+missing, ok when
// installed.
func TestDoctorCheckIndexHooks(t *testing.T) {
	root := t.TempDir()
	if out, err := exec.Command("git", "-C", root, "init").CombinedOutput(); err != nil {
		t.Skipf("git init: %v\n%s", err, out)
	}
	stale := codeGraphFreshness{Status: codeGraphFreshnessStale}
	fresh := codeGraphFreshness{Status: codeGraphFreshnessReady}

	// stale + missing → fail (problems incremented).
	r := &doctorReport{}
	out := captureStdout(t, func() { doctorCheckIndexHooks(root, stale, r) })
	if r.problems != 1 || !strings.Contains(out, "missing and CodeGraph is stale") {
		t.Errorf("stale+missing: problems=%d out=%q", r.problems, out)
	}

	// fresh + missing → warn (no problem increment).
	r = &doctorReport{}
	out = captureStdout(t, func() { doctorCheckIndexHooks(root, fresh, r) })
	if r.problems != 0 || !strings.Contains(out, "not installed") {
		t.Errorf("fresh+missing: problems=%d out=%q", r.problems, out)
	}

	// installed → ok.
	if err := installGitIndexHooks(root); err != nil {
		t.Fatal(err)
	}
	r = &doctorReport{}
	out = captureStdout(t, func() { doctorCheckIndexHooks(root, stale, r) })
	if r.problems != 0 || !strings.Contains(out, "installed (pre-push/post-merge/post-commit") {
		t.Errorf("installed: problems=%d out=%q", r.problems, out)
	}
}

// resetIndexHookFlags restores the package-level init flags/env between tests so
// state does not leak across them.
func resetIndexHookFlags(t *testing.T) {
	t.Helper()
	prevFlag := initNoIndexHooks
	t.Cleanup(func() { initNoIndexHooks = prevFlag })
	initNoIndexHooks = false
	t.Setenv("GG_NO_INDEX_HOOKS", "")
}
