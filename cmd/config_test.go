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
// writes gsd-sonnet-4.6 / cmux when .mcp.json references gsd-workflow and cmux
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
	if cfg.Developer.Agent != "gsd-sonnet-4.6" {
		t.Errorf("Developer.Agent = %q, want gsd-sonnet-4.6", cfg.Developer.Agent)
	}
	if cfg.Developer.Transport != "cmux" {
		t.Errorf("Developer.Transport = %q, want cmux", cfg.Developer.Transport)
	}
}

// TestInitDeveloperConfig_NoGSD verifies that ensureDeveloperConfig writes
// "unconfigured" when GSD is not detected and we are in non-interactive mode.
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
	if cfg.Developer.Agent != "unconfigured" {
		t.Errorf("Developer.Agent = %q, want unconfigured", cfg.Developer.Agent)
	}
}

// TestInitDeveloperConfig_ExistingNotOverwritten verifies that re-running
// ensureDeveloperConfig does not overwrite a value the user already set.
func TestInitDeveloperConfig_ExistingNotOverwritten(t *testing.T) {
	ggDir := setupGGDir(t)

	// Pre-set developer.agent in the config.
	if err := rewriteConfigWithDeveloper(ggDir, "claude-sonnet-4.5", "side-session-prompt"); err != nil {
		t.Fatalf("rewrite config: %v", err)
	}

	if err := ensureDeveloperConfig(nil, ggDir); err != nil {
		t.Fatalf("ensureDeveloperConfig: %v", err)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Developer.Agent != "claude-sonnet-4.5" {
		t.Errorf("Developer.Agent = %q, want claude-sonnet-4.5 (should not be overwritten)", cfg.Developer.Agent)
	}
}

// ── AC-5b: developer.agent override roundtrip via gg config set ──────────────

// TestConfigSet_DeveloperAgent_Valid verifies that a valid agent ID is written
// to .gg/config.yaml and read back correctly.
func TestConfigSet_DeveloperAgent_Valid(t *testing.T) {
	setupGGDir(t)

	_, _, err := execCmd(t, "config", "set", "developer.agent", "claude-sonnet-4.5")
	if err != nil {
		t.Fatalf("gg config set developer.agent: %v", err)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config after set: %v", err)
	}
	if cfg.Developer.Agent != "claude-sonnet-4.5" {
		t.Errorf("Developer.Agent = %q, want claude-sonnet-4.5", cfg.Developer.Agent)
	}
}

// TestConfigSet_DeveloperAgent_RoundTrip verifies that setting then re-reading
// the agent preserves the value.
func TestConfigSet_DeveloperAgent_RoundTrip(t *testing.T) {
	setupGGDir(t)

	for _, agent := range config.ValidDeveloperAgents {
		if _, _, err := execCmd(t, "config", "set", "developer.agent", agent); err != nil {
			t.Fatalf("config set agent=%q: %v", agent, err)
		}
		cfg, err := config.Load()
		if err != nil {
			t.Fatalf("load after set agent=%q: %v", agent, err)
		}
		if cfg.Developer.Agent != agent {
			t.Errorf("round-trip agent: got %q, want %q", cfg.Developer.Agent, agent)
		}
	}
}

// ── AC-5c: status renders developer line ─────────────────────────────────────

// TestStatus_DeveloperLine_Configured verifies renderRolesBlock with an agent+transport.
func TestStatus_DeveloperLine_Configured(t *testing.T) {
	dev := &config.DeveloperConfig{Agent: "gsd-sonnet-4.6", Transport: "cmux"}
	out := renderRolesBlock(dev, "")
	if !strings.Contains(out, "Developer") {
		t.Errorf("expected 'Developer' in Roles block; got:\n%s", out)
	}
	if !strings.Contains(out, "gsd-sonnet-4.6") {
		t.Errorf("expected agent name in Roles block; got:\n%s", out)
	}
	if !strings.Contains(out, "cmux") {
		t.Errorf("expected transport in Roles block; got:\n%s", out)
	}
}

// TestStatus_DeveloperLine_AgentOnlyNoTransport verifies that agent without
// transport renders without parentheses.
func TestStatus_DeveloperLine_AgentOnlyNoTransport(t *testing.T) {
	dev := &config.DeveloperConfig{Agent: "claude-sonnet-4.5"}
	out := renderRolesBlock(dev, "")
	if !strings.Contains(out, "claude-sonnet-4.5") {
		t.Errorf("expected agent name; got:\n%s", out)
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

// ── AC-5d: config validation rejects unknown agent ID ────────────────────────

// TestConfigSet_DeveloperAgent_Invalid verifies that an unknown agent ID is
// rejected with a non-zero exit code and descriptive message.
func TestConfigSet_DeveloperAgent_Invalid(t *testing.T) {
	setupGGDir(t)

	stdout, stderr, err := execCmd(t, "config", "set", "developer.agent", "totally-fake-agent-xyz")
	if err == nil {
		t.Fatal("expected error for invalid agent ID, got nil")
	}
	// Error message is in err.Error(); cobra may also write it to stderr.
	combined := err.Error() + stdout + stderr
	if !strings.Contains(combined, "invalid developer.agent") &&
		!strings.Contains(combined, "totally-fake-agent-xyz") {
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
func rewriteConfigWithDeveloper(ggDir, agent, transport string) error {
	body := ggConfig
	body += "\ndeveloper:\n  agent: " + agent + "\n"
	if transport != "" {
		body += "  transport: " + transport + "\n"
	}
	return os.WriteFile(filepath.Join(ggDir, "config.yaml"), []byte(body), 0o644)
}
