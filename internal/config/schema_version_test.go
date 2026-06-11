package config

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// captureWarnings redirects the config-layer warnSink to a buffer for the
// duration of the test and restores it afterwards.
func captureWarnings(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := warnSink
	warnSink = &buf
	resetWarnedOnce() // isolate per-process dedupe from other tests
	t.Cleanup(func() { warnSink = prev })
	return &buf
}

// validBody is a minimal config that passes Validate() (required
// qdrant/embedding/memgraph fields present). Tests prepend extra lines.
const validBody = `qdrant:
  host: localhost
  port: 6334
embedding:
  host: http://localhost:11434
  model: nomic-embed-text
memgraph:
  uri: bolt://localhost:7687
`

func writeGGConfig(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	ggDir := filepath.Join(dir, ".gg")
	if err := os.MkdirAll(ggDir, 0o755); err != nil {
		t.Fatalf("mkdir .gg: %v", err)
	}
	if err := os.WriteFile(filepath.Join(ggDir, "config.yaml"), []byte(body), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return ggDir
}

// AC-1/AC-4: an old config with no schema_version loads clean (no error, no
// warning) and is stamped to the current baseline by ApplyDefaults.
func TestLoadFromGGDir_OldConfigNoVersionLoadsCleanAndStamps(t *testing.T) {
	warn := captureWarnings(t)
	ggDir := writeGGConfig(t, "project_id: proj\n"+validBody)

	cfg, err := LoadFromGGDir(ggDir)
	if err != nil {
		t.Fatalf("LoadFromGGDir on old config: %v", err)
	}
	if cfg.SchemaVersion != CurrentConfigSchemaVersion {
		t.Errorf("SchemaVersion = %d, want stamped baseline %d", cfg.SchemaVersion, CurrentConfigSchemaVersion)
	}
	if warn.Len() != 0 {
		t.Errorf("expected no warnings for a clean old config, got %q", warn.String())
	}
}

// AC-2: a config with an unrecognised key warns exactly once on stderr and
// still loads successfully with defaults (never hard-fails).
func TestLoadFromGGDir_UnknownKeyWarnsOnceButLoads(t *testing.T) {
	warn := captureWarnings(t)
	ggDir := writeGGConfig(t, "project_id: proj\nbogus_key: 42\nanother_removed: hi\n"+validBody)

	cfg, err := LoadFromGGDir(ggDir)
	if err != nil {
		t.Fatalf("unknown key must not hard-fail load: %v", err)
	}
	if cfg.ProjectID != "proj" {
		t.Errorf("ProjectID = %q, want proj — known keys must still load", cfg.ProjectID)
	}
	out := warn.String()
	if got := strings.Count(out, "unrecognized key"); got != 1 {
		t.Fatalf("expected exactly one unrecognized-key warning, got %d in %q", got, out)
	}
	if !strings.Contains(out, "bogus_key") || !strings.Contains(out, "another_removed") {
		t.Errorf("warning should name the unknown keys, got %q", out)
	}
	if !strings.Contains(out, ".gg/config.yaml") {
		t.Errorf("warning should name the file, got %q", out)
	}
}

// AC-2: repeated loads of the same unknown-key config (as `gg status` does many
// times per invocation) still warn only once per process — pipelines stay clean.
func TestLoadFromGGDir_UnknownKeyWarnsOnlyOnceAcrossLoads(t *testing.T) {
	warn := captureWarnings(t)
	ggDir := writeGGConfig(t, "project_id: proj\nbogus_key: 42\n"+validBody)

	for i := 0; i < 5; i++ {
		if _, err := LoadFromGGDir(ggDir); err != nil {
			t.Fatalf("load %d: %v", i, err)
		}
	}
	if got := strings.Count(warn.String(), "unrecognized key"); got != 1 {
		t.Fatalf("expected exactly one warning across 5 loads, got %d in %q", got, warn.String())
	}
}

// AC-1: a config that already carries a schema_version keeps it (no stamp
// downgrade) and produces no unknown-key warning for that recognised key.
func TestLoadFromGGDir_KnownSchemaVersionKeyNotFlagged(t *testing.T) {
	warn := captureWarnings(t)
	ggDir := writeGGConfig(t, "schema_version: 1\nproject_id: proj\n"+validBody)

	cfg, err := LoadFromGGDir(ggDir)
	if err != nil {
		t.Fatalf("LoadFromGGDir: %v", err)
	}
	if cfg.SchemaVersion != 1 {
		t.Errorf("SchemaVersion = %d, want 1", cfg.SchemaVersion)
	}
	if warn.Len() != 0 {
		t.Errorf("schema_version is a known key — should not warn, got %q", warn.String())
	}
}

// AC-3: Save stamps the current registry schema version.
func TestRegistry_SaveStampsSchemaVersion(t *testing.T) {
	withTempHome(t)
	reg, _ := LoadRegistry()
	reg.Add("proj-1", "/tmp/proj-1-root")
	if err := reg.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}
	if reg.SchemaVersion != CurrentRegistrySchemaVersion {
		t.Errorf("in-memory SchemaVersion = %d, want %d", reg.SchemaVersion, CurrentRegistrySchemaVersion)
	}
	got, err := LoadRegistry()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got.SchemaVersion != CurrentRegistrySchemaVersion {
		t.Errorf("persisted SchemaVersion = %d, want %d", got.SchemaVersion, CurrentRegistrySchemaVersion)
	}
}

// AC-3/AC-4: an old projects.json with no version field loads clean (baseline,
// no warning).
func TestLoadRegistry_OldNoVersionLoadsClean(t *testing.T) {
	withTempHome(t)
	warn := captureWarnings(t)
	p, err := registryPath()
	if err != nil {
		t.Fatalf("registryPath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// legacy shape: no schema_version key at all
	if err := os.WriteFile(p, []byte(`{"projects":{"a":{"id":"a","root":"/r","name":"r","registered_at":"t"}}}`), 0o644); err != nil {
		t.Fatalf("write legacy registry: %v", err)
	}
	reg, err := LoadRegistry()
	if err != nil {
		t.Fatalf("LoadRegistry on legacy file: %v", err)
	}
	if reg.SchemaVersion != 0 {
		t.Errorf("legacy registry SchemaVersion = %d, want 0 (baseline, unstamped)", reg.SchemaVersion)
	}
	if len(reg.Projects) != 1 {
		t.Errorf("expected 1 project, got %d", len(reg.Projects))
	}
	if warn.Len() != 0 {
		t.Errorf("legacy (older) registry must not warn, got %q", warn.String())
	}
}

// AC-3/AC-4: a registry whose schema_version is newer than this binary still
// loads, but emits one stderr warning.
func TestLoadRegistry_FutureVersionWarnsButLoads(t *testing.T) {
	withTempHome(t)
	warn := captureWarnings(t)
	p, err := registryPath()
	if err != nil {
		t.Fatalf("registryPath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	future := Registry{SchemaVersion: CurrentRegistrySchemaVersion + 5, Projects: map[string]ProjectEntry{}}
	b, _ := json.Marshal(future)
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatalf("write future registry: %v", err)
	}
	reg, err := LoadRegistry()
	if err != nil {
		t.Fatalf("future registry must not hard-fail: %v", err)
	}
	if reg.SchemaVersion != CurrentRegistrySchemaVersion+5 {
		t.Errorf("SchemaVersion = %d, want preserved future value", reg.SchemaVersion)
	}
	if got := strings.Count(warn.String(), "newer than this gg understands"); got != 1 {
		t.Errorf("expected one future-version warning, got %d in %q", got, warn.String())
	}
}
