// Package cmd — tests for TASK-321: developer config block, gg config set, status Roles line.
package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gurkangul/gg-cli/internal/config"
)

// ── AC-5a: init writes developer config with default ─────────────────────────

// TestInitDeveloperConfig_GSDAndCmuxPresent verifies that ensureDeveloperConfig
// writes a generic gsd command / cmux when .mcp.json references gsd-workflow and cmux
// is on PATH (simulated via a fake cmux on PATH).
func TestInitDeveloperConfig_GSDAndCmuxPresent(t *testing.T) {
	ggDir := setupGGDir(t)
	root := filepath.Dir(ggDir)

	// Simulate GSD presence via .mcp.json.
	mcpJSON := `{"mcpServers":{"gsd-workflow":{"command":"node"}}}`
	if err := os.WriteFile(filepath.Join(root, ".mcp.json"), []byte(mcpJSON), 0o644); err != nil {
		t.Fatalf("write .mcp.json: %v", err)
	}

	// Fake cmux binary in a temp dir on PATH.
	fakeBin := t.TempDir()
	cmuxPath := filepath.Join(fakeBin, "cmux")
	if err := os.WriteFile(cmuxPath, []byte("#!/bin/sh\necho fake\n"), 0o755); err != nil {
		t.Fatalf("write fake cmux: %v", err)
	}
	origPath := os.Getenv("PATH")
	t.Setenv("PATH", fakeBin+":"+origPath)

	// Remove developer block from config so ensureDeveloperConfig runs.
	if err := rewriteConfigWithoutDeveloper(ggDir); err != nil {
		t.Fatalf("rewrite config: %v", err)
	}

	if err := ensureDeveloperConfig(nil, ggDir); err != nil {
		t.Fatalf("ensureDeveloperConfig: %v", err)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Developer.Command != "gsd" {
		t.Errorf("Developer.Command = %q, want gsd", cfg.Developer.Command)
	}
	if cfg.Developer.Transport != "cmux" {
		t.Errorf("Developer.Transport = %q, want cmux", cfg.Developer.Transport)
	}
}

// TestInitDeveloperConfig_NoGSD verifies that ensureDeveloperConfig writes
// no command when GSD is not detected and we are in non-interactive mode.
func TestInitDeveloperConfig_NoGSD(t *testing.T) {
	ggDir := setupGGDir(t)

	// Remove developer block so detection runs.
	if err := rewriteConfigWithoutDeveloper(ggDir); err != nil {
		t.Fatalf("rewrite config: %v", err)
	}

	// Force non-interactive path via initYes=true.
	origYes := initYes
	defer func() { initYes = origYes }()
	initYes = true

	// Isolate PATH so cmux is not found even if installed on the test host.
	t.Setenv("PATH", t.TempDir())

	// No .mcp.json, no cmux binary → GSD and cmux both absent.
	if err := ensureDeveloperConfig(nil, ggDir); err != nil {
		t.Fatalf("ensureDeveloperConfig: %v", err)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Developer.Command != "" {
		t.Errorf("Developer.Command = %q, want empty", cfg.Developer.Command)
	}
}

// TestInitDeveloperConfig_ExistingNotOverwritten verifies that re-running
// ensureDeveloperConfig does not overwrite a value the user already set.
func TestInitDeveloperConfig_ExistingNotOverwritten(t *testing.T) {
	ggDir := setupGGDir(t)

	// Pre-set developer.command in the config.
	if err := rewriteConfigWithDeveloper(ggDir, "codex --model gpt-5.3-codex", "side-session-prompt"); err != nil {
		t.Fatalf("rewrite config: %v", err)
	}

	if err := ensureDeveloperConfig(nil, ggDir); err != nil {
		t.Fatalf("ensureDeveloperConfig: %v", err)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Developer.Command != "codex --model gpt-5.3-codex" {
		t.Errorf("Developer.Command = %q, want codex command (should not be overwritten)", cfg.Developer.Command)
	}
}

// ── AC-5b: developer.command override roundtrip via gg config set ────────────

// TestConfigSet_DeveloperCommand_Valid verifies that a command is written
// to .gg/config.yaml and read back correctly.
func TestConfigSet_DeveloperCommand_Valid(t *testing.T) {
	setupGGDir(t)

	_, _, err := execCmd(t, "config", "set", "developer.command", "gsd --model openai-codex/gpt-5.3-codex")
	if err != nil {
		t.Fatalf("gg config set developer.command: %v", err)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config after set: %v", err)
	}
	if cfg.Developer.Command != "gsd --model openai-codex/gpt-5.3-codex" {
		t.Errorf("Developer.Command = %q, want gsd command", cfg.Developer.Command)
	}
}

func TestConfigSet_RoleReviewerCommand_Valid(t *testing.T) {
	setupGGDir(t)

	_, _, err := execCmd(t, "config", "set", "roles.reviewer.command", "codex --model gpt-5.3-codex")
	if err != nil {
		t.Fatalf("gg config set roles.reviewer.command: %v", err)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config after set: %v", err)
	}
	if cfg.Roles["reviewer"].Command != "codex --model gpt-5.3-codex" {
		t.Errorf("reviewer command = %q, want codex command", cfg.Roles["reviewer"].Command)
	}
}

func TestConfigSet_RoleMasterCommand_Valid(t *testing.T) {
	setupGGDir(t)

	_, _, err := execCmd(t, "config", "set", "roles.master.command", "codex --model gpt-5.5")
	if err != nil {
		t.Fatalf("gg config set roles.master.command: %v", err)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config after set: %v", err)
	}
	if cfg.Roles["master"].Command != "codex --model gpt-5.5" {
		t.Errorf("master command = %q, want codex command", cfg.Roles["master"].Command)
	}
}

func TestConfigSet_RuntimeProfileFields_Valid(t *testing.T) {
	setupGGDir(t)

	sets := [][]string{
		{"runtime_profiles.gsd-dev.command", "gsd --model openai-codex/gpt-5.3-codex"},
		{"runtime_profiles.gsd-dev.role", "developer"},
		{"runtime_profiles.gsd-dev.priority", "10"},
		{"runtime_profiles.gsd-dev.health_command", "ggdev-worker health"},
	}
	for _, s := range sets {
		if _, _, err := execCmd(t, "config", "set", s[0], s[1]); err != nil {
			t.Fatalf("gg config set %s: %v", s[0], err)
		}
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config after set: %v", err)
	}
	profile := cfg.RuntimeProfiles["gsd-dev"]
	if profile.Command != "gsd --model openai-codex/gpt-5.3-codex" || profile.Role != "developer" || profile.Priority != 10 || profile.HealthCommand != "ggdev-worker health" {
		t.Fatalf("profile mismatch: %#v", profile)
	}
}

func TestConfigSet_BackupFields_Valid(t *testing.T) {
	setupGGDir(t)

	if _, _, err := execCmd(t, "config", "set", "backup.enabled", "false"); err != nil {
		t.Fatalf("gg config set backup.enabled: %v", err)
	}
	if _, _, err := execCmd(t, "config", "set", "backup.interval", "6h"); err != nil {
		t.Fatalf("gg config set backup.interval: %v", err)
	}
	if _, _, err := execCmd(t, "config", "set", "backup.timeout", "45s"); err != nil {
		t.Fatalf("gg config set backup.timeout: %v", err)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config after set: %v", err)
	}
	if cfg.Backup.AutoEnabled() {
		t.Fatal("Backup.AutoEnabled() = true, want false")
	}
	if cfg.Backup.Interval != "6h" {
		t.Fatalf("Backup.Interval = %q, want 6h", cfg.Backup.Interval)
	}
	if cfg.Backup.Timeout != "45s" {
		t.Fatalf("Backup.Timeout = %q, want 45s", cfg.Backup.Timeout)
	}
}

func TestConfigSet_BackupInterval_Invalid(t *testing.T) {
	setupGGDir(t)

	_, _, err := execCmd(t, "config", "set", "backup.interval", "tomorrow")
	if err == nil {
		t.Fatal("expected invalid backup.interval to fail")
	}
}

// TestConfigSet_DeveloperCommand_RoundTrip verifies that setting then re-reading
// the command preserves arbitrary agent subprocess values.
func TestConfigSet_DeveloperCommand_RoundTrip(t *testing.T) {
	setupGGDir(t)

	for _, command := range []string{"gsd", "codex --model gpt-5.3-codex", "./my-agent-team run developer"} {
		if _, _, err := execCmd(t, "config", "set", "developer.command", command); err != nil {
			t.Fatalf("config set command=%q: %v", command, err)
		}
		cfg, err := config.Load()
		if err != nil {
			t.Fatalf("load after set command=%q: %v", command, err)
		}
		if cfg.Developer.Command != command {
			t.Errorf("round-trip command: got %q, want %q", cfg.Developer.Command, command)
		}
	}
}

func TestConfigSet_LegacyDeveloperAgentUnconfiguredClearsCommand(t *testing.T) {
	setupGGDir(t)

	if _, _, err := execCmd(t, "config", "set", "developer.agent", "unconfigured"); err != nil {
		t.Fatalf("config set legacy unconfigured: %v", err)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load after legacy unconfigured: %v", err)
	}
	if cfg.Developer.Command != "" {
		t.Errorf("Developer.Command = %q, want empty", cfg.Developer.Command)
	}
	if line := developerCommand(&cfg.Developer); line != "" {
		t.Errorf("developerCommand = %q, want empty", line)
	}
}

// ── AC-5c: status renders developer line ─────────────────────────────────────

// TestStatus_DeveloperLine_Configured verifies renderRolesBlock with a command+transport.
func TestStatus_DeveloperLine_Configured(t *testing.T) {
	dev := &config.DeveloperConfig{Command: "gsd --model openai-codex/gpt-5.3-codex", Transport: "cmux"}
	out := renderRolesBlock(dev, "")
	if !strings.Contains(out, "Developer") {
		t.Errorf("expected 'Developer' in Roles block; got:\n%s", out)
	}
	if !strings.Contains(out, "gsd --model openai-codex/gpt-5.3-codex") {
		t.Errorf("expected command in Roles block; got:\n%s", out)
	}
	if !strings.Contains(out, "cmux") {
		t.Errorf("expected transport in Roles block; got:\n%s", out)
	}
}

// TestStatus_DeveloperLine_AgentOnlyNoTransport verifies that command without
// transport renders without parentheses.
func TestStatus_DeveloperLine_AgentOnlyNoTransport(t *testing.T) {
	dev := &config.DeveloperConfig{Command: "codex --model gpt-5.3-codex"}
	out := renderRolesBlock(dev, "")
	if !strings.Contains(out, "codex --model gpt-5.3-codex") {
		t.Errorf("expected command; got:\n%s", out)
	}
	if strings.Contains(out, "(") {
		t.Errorf("expected no parentheses when transport empty; got:\n%s", out)
	}
}

// TestStatus_DeveloperLine_Unconfigured verifies the warning when agent is empty.
func TestStatus_DeveloperLine_Unconfigured(t *testing.T) {
	dev := &config.DeveloperConfig{}
	out := renderRolesBlock(dev, "")
	if !strings.Contains(out, "unconfigured") {
		t.Errorf("expected 'unconfigured' warning; got:\n%s", out)
	}
}

// TestStatus_DeveloperLine_UnconfiguredValue verifies explicit "unconfigured" value.
func TestStatus_DeveloperLine_UnconfiguredValue(t *testing.T) {
	dev := &config.DeveloperConfig{Agent: "unconfigured"}
	out := renderRolesBlock(dev, "")
	if !strings.Contains(out, "unconfigured") {
		t.Errorf("expected 'unconfigured' in output; got:\n%s", out)
	}
}

// ── AC-5d: config validation rejects empty command ───────────────────────────

// TestConfigSet_DeveloperCommand_Invalid verifies that an empty command is
// rejected with a non-zero exit code and descriptive message.
func TestConfigSet_DeveloperCommand_Invalid(t *testing.T) {
	setupGGDir(t)

	stdout, stderr, err := execCmd(t, "config", "set", "developer.command", " ")
	if err == nil {
		t.Fatal("expected error for invalid command, got nil")
	}
	// Error message is in err.Error(); cobra may also write it to stderr.
	combined := err.Error() + stdout + stderr
	if !strings.Contains(combined, "invalid developer.command") {
		t.Errorf("expected rejection message in error/output; err=%v stdout=%s stderr=%s", err, stdout, stderr)
	}
}

// TestConfigSet_UnknownKey verifies that an unknown config key is rejected.
func TestConfigSet_UnknownKey(t *testing.T) {
	setupGGDir(t)

	_, _, err := execCmd(t, "config", "set", "nonexistent.key", "value")
	if err == nil {
		t.Fatal("expected error for unknown key, got nil")
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

// rewriteConfigWithoutDeveloper writes a minimal config that omits the
// developer block entirely so ensureDeveloperConfig triggers detection.
func rewriteConfigWithoutDeveloper(ggDir string) error {
	return os.WriteFile(filepath.Join(ggDir, "config.yaml"), []byte(ggConfig), 0o644)
}

// rewriteConfigWithDeveloper writes a config that already has a developer block.
func rewriteConfigWithDeveloper(ggDir, command, transport string) error {
	body := ggConfig
	body += "\ndeveloper:\n  command: " + command + "\n"
	if transport != "" {
		body += "  transport: " + transport + "\n"
	}
	return os.WriteFile(filepath.Join(ggDir, "config.yaml"), []byte(body), 0o644)
}
