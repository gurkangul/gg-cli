package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gurkangul/gg-cli/internal/agenthooks"
	"github.com/gurkangul/gg-cli/internal/orchestrator/spawn"
)

// TestBecomeMaster_InstallsBlockAndHeartbeat is the primary AC test:
// running `gg become master` in a project with no master-role block must
// install the block in CLAUDE.md and write a heartbeat file.
//
// AC-1: command exists and exits 0
// AC-2: master-role block installed in CLAUDE.md (MISSING → OK)
// AC-3: heartbeat.json written to spawn dir
func TestBecomeMaster_InstallsBlockAndHeartbeat(t *testing.T) {
	setupGGDir(t)
	t.Setenv("GG_AGENT", "test-master")

	stdout, stderr, err := execCmd(t, "become", "master")
	if err != nil {
		t.Fatalf("AC-1: become master failed: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	// AC-2: CLAUDE.md now has a current master-role block.
	cwd, _ := os.Getwd()
	r := agenthooks.CheckMasterRole(cwd)
	if r.Status != agenthooks.MasterRoleOK {
		t.Errorf("AC-2: master-role block status = %s after become master, want OK", r.Status)
	}

	// AC-3: heartbeat.json was written.
	rt := testRuntimeDir(t)
	hb, err := spawn.ReadHeartbeat(rt)
	if err != nil {
		t.Fatalf("AC-3: ReadHeartbeat failed: %v (expected heartbeat.json to exist)", err)
	}
	if hb.Agent != "test-master" {
		t.Errorf("AC-3: heartbeat.Agent = %q, want %q", hb.Agent, "test-master")
	}
	if hb.UpdatedAt.IsZero() {
		t.Error("AC-3: heartbeat.UpdatedAt is zero")
	}
}

// TestBecomeMaster_Idempotent verifies that running become master twice does
// not corrupt CLAUDE.md or produce an error.
func TestBecomeMaster_Idempotent(t *testing.T) {
	setupGGDir(t)

	for i := 0; i < 2; i++ {
		_, _, err := execCmd(t, "become", "master")
		if err != nil {
			t.Fatalf("run %d: become master failed: %v", i+1, err)
		}
	}

	cwd, _ := os.Getwd()
	r := agenthooks.CheckMasterRole(cwd)
	if r.Status != agenthooks.MasterRoleOK {
		t.Errorf("after 2 runs: master-role status = %s, want OK", r.Status)
	}
}

// TestBecomeMaster_ExistingCLAUDEMD verifies that become master appends to an
// existing CLAUDE.md without clobbering pre-existing content.
func TestBecomeMaster_ExistingCLAUDEMD(t *testing.T) {
	setupGGDir(t)

	cwd, _ := os.Getwd()
	claudeMD := filepath.Join(cwd, "CLAUDE.md")
	existing := "# Existing Project Instructions\n\nSome existing content.\n"
	if err := os.WriteFile(claudeMD, []byte(existing), 0o644); err != nil {
		t.Fatalf("write CLAUDE.md: %v", err)
	}

	_, _, err := execCmd(t, "become", "master")
	if err != nil {
		t.Fatalf("become master failed: %v", err)
	}

	data, err := os.ReadFile(claudeMD)
	if err != nil {
		t.Fatalf("read CLAUDE.md: %v", err)
	}
	content := string(data)

	if !strings.Contains(content, existing) {
		t.Error("become master clobbered pre-existing CLAUDE.md content")
	}
	if !strings.Contains(content, agenthooks.MasterRoleBlockBegin) {
		t.Error("become master did not append master-role block to existing CLAUDE.md")
	}
}

// TestBecomeMaster_NoGGDir verifies that become master returns a non-nil error
// when there is no .gg/ directory (project not initialised).
func TestBecomeMaster_NoGGDir(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	_, _, err := execCmd(t, "become", "master")
	if err == nil {
		t.Fatal("expected error when .gg/ is absent, got nil")
	}
}

// TestBecome_NoArg verifies that `gg become` with no subcommand exits 0 and
// prints usage help (AC-4).
func TestBecome_NoArg(t *testing.T) {
	stdout, stderr, err := execCmd(t, "become")
	if err != nil {
		t.Fatalf("AC-4: gg become (no arg) exited non-zero: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}
	combined := stdout + stderr
	if !strings.Contains(combined, "master") {
		t.Errorf("AC-4: expected 'master' in help output, got:\n%s", combined)
	}
}

// TestBecome_NoArg_WithDeveloperAgent verifies that when developer.agent is set
// in config, `gg become` (no subcommand) prints the role hint before help.
func TestBecome_NoArg_WithDeveloperAgent(t *testing.T) {
	ggDir := setupGGDir(t)
	cfgWithAgent := ggConfig + "developer:\n  agent: gsd-sonnet-4.6\n"
	if err := os.WriteFile(filepath.Join(ggDir, "config.yaml"), []byte(cfgWithAgent), 0o644); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}

	stdout, stderr, err := execCmd(t, "become")
	if err != nil {
		t.Fatalf("gg become exited non-zero: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}
	combined := stdout + stderr
	if !strings.Contains(combined, "gsd-sonnet-4.6") {
		t.Errorf("expected role hint 'gsd-sonnet-4.6' in output, got:\n%s", combined)
	}
}

// TestBecomeMaster_HelpMentionsOptIn verifies AC-5(a): the Long description of
// `gg become master` explicitly mentions that master discipline is opt-in.
func TestBecomeMaster_HelpMentionsOptIn(t *testing.T) {
	stdout, stderr, err := execCmd(t, "become", "master", "--help")
	if err != nil {
		t.Fatalf("AC-5: gg become master --help exited non-zero: %v", err)
	}
	combined := stdout + stderr
	if !strings.Contains(strings.ToLower(combined), "opt-in") {
		t.Errorf("AC-5: expected 'opt-in' in help text, got:\n%s", combined)
	}
	if !strings.Contains(combined, "master discipline") {
		t.Errorf("AC-5: expected 'master discipline' in help text, got:\n%s", combined)
	}
}

// TestBecomeMaster_PrintsMasterPrompt verifies AC-2: running `gg become master`
// prints the paste-ready master session prompt to stdout.
func TestBecomeMaster_PrintsMasterPrompt(t *testing.T) {
	setupGGDir(t)

	stdout, stderr, err := execCmd(t, "become", "master")
	if err != nil {
		t.Fatalf("AC-2: become master failed: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}
	combined := stdout + stderr
	if !strings.Contains(combined, "You are now the MASTER session") {
		t.Errorf("AC-2: expected master prompt in output, got:\n%s", combined)
	}
	if !strings.Contains(combined, "gg:master-role:begin v3") {
		t.Errorf("AC-2: expected marker reference in master prompt, got:\n%s", combined)
	}
	if !strings.Contains(combined, "Heartbeat starting now") {
		t.Errorf("AC-2: expected 'Heartbeat starting now' in master prompt, got:\n%s", combined)
	}
}

// TestBecome_NoArg_NoDeveloperAgent verifies that when developer.agent is absent
// or "unconfigured", `gg become` prints the "no role declared" fallback message.
func TestBecome_NoArg_NoDeveloperAgent(t *testing.T) {
	setupGGDir(t)

	stdout, stderr, err := execCmd(t, "become")
	if err != nil {
		t.Fatalf("gg become exited non-zero: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}
	combined := stdout + stderr
	if !strings.Contains(combined, "no role declared") {
		t.Errorf("expected 'no role declared' in output, got:\n%s", combined)
	}
}
