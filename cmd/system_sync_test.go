// Package cmd — tests for `gg system sync` flag propagation.
package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gurkangul/gg-cli/internal/config"
)

// systemSyncFixture builds a temp HOME with a two-project registry and two
// matching project directories (each with a .gg/config.yaml), then changes
// into one of them so LoadRegistry succeeds.
func systemSyncFixture(t *testing.T) (proj1Root string, proj2Root string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Two fake project roots with minimal .gg/config.yaml so the existence
	// check inside runSystemSync passes.
	proj1Root = t.TempDir()
	proj2Root = t.TempDir()

	for _, root := range []string{proj1Root, proj2Root} {
		ggDir := filepath.Join(root, ".gg")
		if err := os.MkdirAll(ggDir, 0o755); err != nil {
			t.Fatalf("mkdir .gg: %v", err)
		}
		if err := os.WriteFile(filepath.Join(ggDir, "config.yaml"), []byte(ggConfig), 0o644); err != nil {
			t.Fatalf("write config.yaml: %v", err)
		}
	}

	// Write the registry to the shared dir LoadRegistry reads.
	sharedDir := filepath.Join(home, ".gg")
	if err := os.MkdirAll(sharedDir, 0o755); err != nil {
		t.Fatalf("mkdir shared: %v", err)
	}
	reg := config.Registry{
		Projects: map[string]config.ProjectEntry{
			"proj-alpha": {
				ID:           "proj-alpha",
				Root:         proj1Root,
				Name:         "alpha",
				RegisteredAt: time.Now().UTC().Format(time.RFC3339),
			},
			"proj-beta": {
				ID:           "proj-beta",
				Root:         proj2Root,
				Name:         "beta",
				RegisteredAt: time.Now().UTC().Format(time.RFC3339),
			},
		},
	}
	data, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		t.Fatalf("marshal registry: %v", err)
	}
	regPath := filepath.Join(sharedDir, config.RegistryFile)
	if err := os.WriteFile(regPath, data, 0o644); err != nil {
		t.Fatalf("write registry: %v", err)
	}

	t.Chdir(proj1Root)
	return proj1Root, proj2Root
}

// runSystemSyncCapture invokes runSystemSync with the package-level flag vars
// preset, capturing everything written to os.Stdout. This is necessary because
// the command uses fmt.Print* which bypasses cobra's SetOut writer.
func runSystemSyncCapture(t *testing.T) string {
	t.Helper()

	// Redirect os.Stdout → pipe so fmt.Print* inside runSystemSync is captured.
	origStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w

	runErr := runSystemSync(systemSyncCmd, nil)

	w.Close()
	os.Stdout = origStdout

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		t.Fatalf("read captured stdout: %v", err)
	}
	if runErr != nil {
		t.Fatalf("runSystemSync: %v", runErr)
	}
	return buf.String()
}

// TestSystemSync_DryRun_DefaultNoForceReset verifies that without any
// force-reset flags the dry-run output does NOT include --force-reset.
func TestSystemSync_DryRun_DefaultNoForceReset(t *testing.T) {
	systemSyncFixture(t)

	// Set flag vars directly (reset in deferred cleanup).
	systemSyncDryRun = true
	systemSyncContractForceReset = false
	systemSyncMasterRoleForceReset = false
	systemSyncContractOnly = false
	systemSyncSkipMasterRole = false
	t.Cleanup(func() {
		systemSyncDryRun = false
		systemSyncContractForceReset = false
		systemSyncMasterRoleForceReset = false
		systemSyncContractOnly = false
		systemSyncSkipMasterRole = false
	})

	out := runSystemSyncCapture(t)

	if strings.Contains(out, "--force-reset") {
		t.Errorf("default dry-run should not include --force-reset, got:\n%s", out)
	}
	if !strings.Contains(out, "--check-contract") {
		t.Errorf("expected --check-contract in dry-run output, got:\n%s", out)
	}
	if !strings.Contains(out, "--check-master-role") {
		t.Errorf("expected --check-master-role in dry-run output, got:\n%s", out)
	}
}

// TestSystemSync_DryRun_ContractForceReset verifies --contract-force-reset
// appends --force-reset to the contract doctor invocation only.
func TestSystemSync_DryRun_ContractForceReset(t *testing.T) {
	systemSyncFixture(t)

	systemSyncDryRun = true
	systemSyncContractForceReset = true
	systemSyncMasterRoleForceReset = false
	systemSyncContractOnly = false
	systemSyncSkipMasterRole = false
	t.Cleanup(func() {
		systemSyncDryRun = false
		systemSyncContractForceReset = false
		systemSyncMasterRoleForceReset = false
		systemSyncContractOnly = false
		systemSyncSkipMasterRole = false
	})

	out := runSystemSyncCapture(t)

	if !strings.Contains(out, "--check-contract --fix --force-reset") {
		t.Errorf("expected contract stage to include --force-reset, got:\n%s", out)
	}
	// Master-role must NOT carry --force-reset.
	if strings.Contains(out, "--check-master-role --fix --force-reset") {
		t.Errorf("master-role stage should not include --force-reset, got:\n%s", out)
	}
}

// TestSystemSync_DryRun_MasterRoleForceReset verifies --master-role-force-reset
// appends --force-reset to the master-role doctor invocation only.
func TestSystemSync_DryRun_MasterRoleForceReset(t *testing.T) {
	systemSyncFixture(t)

	systemSyncDryRun = true
	systemSyncContractForceReset = false
	systemSyncMasterRoleForceReset = true
	systemSyncContractOnly = false
	systemSyncSkipMasterRole = false
	t.Cleanup(func() {
		systemSyncDryRun = false
		systemSyncContractForceReset = false
		systemSyncMasterRoleForceReset = false
		systemSyncContractOnly = false
		systemSyncSkipMasterRole = false
	})

	out := runSystemSyncCapture(t)

	if !strings.Contains(out, "--check-master-role --fix --force-reset") {
		t.Errorf("expected master-role stage to include --force-reset, got:\n%s", out)
	}
	// Contract must NOT carry --force-reset.
	if strings.Contains(out, "--check-contract --fix --force-reset") {
		t.Errorf("contract stage should not include --force-reset, got:\n%s", out)
	}
}

// TestSystemSync_DryRun_BothForceReset verifies both flags together add
// --force-reset to both stages — the v1→v2 upgrade scenario from the task spec.
func TestSystemSync_DryRun_BothForceReset(t *testing.T) {
	systemSyncFixture(t)

	systemSyncDryRun = true
	systemSyncContractForceReset = true
	systemSyncMasterRoleForceReset = true
	systemSyncContractOnly = false
	systemSyncSkipMasterRole = false
	t.Cleanup(func() {
		systemSyncDryRun = false
		systemSyncContractForceReset = false
		systemSyncMasterRoleForceReset = false
		systemSyncContractOnly = false
		systemSyncSkipMasterRole = false
	})

	out := runSystemSyncCapture(t)

	if !strings.Contains(out, "--check-contract --fix --force-reset") {
		t.Errorf("expected contract stage with --force-reset, got:\n%s", out)
	}
	if !strings.Contains(out, "--check-master-role --fix --force-reset") {
		t.Errorf("expected master-role stage with --force-reset, got:\n%s", out)
	}
}

// TestSystemSync_DryRun_TwoProjects_BothGetForceReset is the integration
// fixture simulating a v1→v2 upgrade across 2 projects.  Both projects must
// show the force-reset propagation in their dry-run output.
func TestSystemSync_DryRun_TwoProjects_BothGetForceReset(t *testing.T) {
	proj1Root, proj2Root := systemSyncFixture(t)

	systemSyncDryRun = true
	systemSyncContractForceReset = false
	systemSyncMasterRoleForceReset = true
	systemSyncContractOnly = false
	systemSyncSkipMasterRole = false
	t.Cleanup(func() {
		systemSyncDryRun = false
		systemSyncContractForceReset = false
		systemSyncMasterRoleForceReset = false
		systemSyncContractOnly = false
		systemSyncSkipMasterRole = false
	})

	out := runSystemSyncCapture(t)

	// Both project roots must appear in the bullet line.
	for _, root := range []string{proj1Root, proj2Root} {
		if !strings.Contains(out, root) {
			t.Errorf("expected project root %s in output, got:\n%s", root, out)
		}
	}

	// --check-master-role --fix --force-reset must appear once per project (2 total).
	count := strings.Count(out, "--check-master-role --fix --force-reset")
	if count != 2 {
		t.Errorf("expected --check-master-role --fix --force-reset to appear twice, got %d:\n%s", count, out)
	}
}

// TestSystemSync_NoProjects_PrintsHelp verifies the no-projects fast-path
// does not error.
func TestSystemSync_NoProjects_PrintsHelp(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Chdir(t.TempDir())
	// No registry file → empty registry.

	origStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w

	runErr := runSystemSync(systemSyncCmd, nil)

	w.Close()
	os.Stdout = origStdout

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		t.Fatalf("read: %v", err)
	}
	if runErr != nil {
		t.Fatalf("unexpected error: %v", runErr)
	}
	if !strings.Contains(buf.String(), "No projects registered") {
		t.Errorf("expected 'No projects registered', got:\n%s", buf.String())
	}
}
