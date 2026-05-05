// Package cmd — shared test fixtures: minimal config constant and setupGGDir helper.
package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

// ggConfig is a minimal valid config that points Qdrant at a port with nothing
// listening (19997) so HealthCheck fails immediately.
const ggConfig = `
project_id: test-project-fixture
qdrant:
  host: "127.0.0.1"
  port: 19997
embedding:
  host: "http://localhost:11434"
  model: "nomic-embed-text"
memgraph:
  uri: "bolt://localhost:1"
`

// testProjectID must match the project_id in ggConfig.
const testProjectID = "test-project-fixture"

// setupGGDir writes a minimal .gg/config.yaml into a fresh temp dir, changes
// the test's working directory to that temp dir, sets HOME to a separate temp
// dir so config.RuntimeDir() is isolated per test without colliding with the
// project .gg/, resets the inherited agent role, and returns the .gg path.
// The test's Cleanup restores the original working directory and HOME automatically.
func setupGGDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	home := t.TempDir() // separate from dir so SharedDir() != project .gg/
	ggDir := filepath.Join(dir, ".gg")
	if err := os.MkdirAll(ggDir, 0o755); err != nil {
		t.Fatalf("mkdir .gg: %v", err)
	}
	cfgPath := filepath.Join(ggDir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(ggConfig), 0o644); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}
	t.Setenv("HOME", home)
	t.Setenv("GG_AGENT", "test")
	t.Setenv("GG_ROLE", "")
	t.Chdir(dir)
	return ggDir
}

// testRuntimeDir returns the per-test runtime directory that config.RuntimeDir()
// will produce for the fixture project. HOME must have been set by setupGGDir.
func testRuntimeDir(t *testing.T) string {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	return filepath.Join(home, ".gg", "projects", testProjectID)
}
