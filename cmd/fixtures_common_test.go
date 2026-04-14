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

// setupGGDir writes a minimal .gg/config.yaml into a fresh temp dir, changes
// the test's working directory to that temp dir, and returns the .gg path.
// The test's Cleanup restores the original working directory automatically
// via t.Chdir.
func setupGGDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	ggDir := filepath.Join(dir, ".gg")
	if err := os.MkdirAll(ggDir, 0o755); err != nil {
		t.Fatalf("mkdir .gg: %v", err)
	}
	cfgPath := filepath.Join(ggDir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(ggConfig), 0o644); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}
	t.Chdir(dir)
	return ggDir
}
