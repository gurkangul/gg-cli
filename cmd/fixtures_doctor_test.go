// Package cmd — tests for the doctor command and parseAgentsSchema helper.
package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gurkangul/gg-cli/internal/outbox"
)

// configNoMemgraph is a config without a memgraph URI — exercises the
// doctorCheckConfig "memgraph.uri not set" warning and the
// doctorCheckMemgraph "skipped" early-return path.
const configNoMemgraph = `
project_id: test-no-memgraph
qdrant:
  host: "127.0.0.1"
  port: 19997
embedding:
  host: "http://localhost:11434"
  model: "nomic-embed-text"
`

// configMissingProjectID is a config without project_id — exercises the
// doctorCheckConfig ErrMissingProjectID path and the runDoctor "cfg==nil" else branch.
const configMissingProjectID = `
qdrant:
  host: "127.0.0.1"
  port: 19997
embedding:
  host: "http://localhost:11434"
  model: "nomic-embed-text"
`

// ── doctor command ────────────────────────────────────────────────────────────

// TestDoctor_ServicesDown verifies that 'gg doctor' runs end-to-end when all
// services (Qdrant, Memgraph, Ollama) are unreachable. It should return a
// non-nil error listing the number of problems found, but NOT panic.
func TestDoctor_ServicesDown(t *testing.T) {
	setupGGDir(t)
	_, _, err := execCmd(t, "doctor")
	// doctor does not return ExitStoreDown — it returns a plain error counting
	// the number of problems. With three unreachable services the error is non-nil.
	// We just verify it did not panic or return an ExitStoreDown.
	if err != nil {
		if ee, ok := err.(*ExitError); ok {
			if ee.Code == ExitStoreDown {
				t.Errorf("doctor should not return ExitStoreDown, got: %v", err)
			}
		}
	}
}

func TestDoctor_Reconcile_ServicesDown(t *testing.T) {
	setupGGDir(t)
	// --reconcile lists outbox entries; on a fresh .gg dir the outbox is empty.
	_, _, err := execCmd(t, "doctor", "--reconcile")
	// Expect nil error (empty outbox on fresh dir) or a wrapped GGDir error.
	if err != nil {
		if ee, ok := err.(*ExitError); ok && ee.Code == ExitStoreDown {
			t.Errorf("doctor --reconcile should not return ExitStoreDown, got: %v", err)
		}
	}
}

func TestDoctor_WithAgentsMD(t *testing.T) {
	ggDir := setupGGDir(t)
	root := filepath.Dir(ggDir)

	// Write a minimal AGENTS.md with schema frontmatter so doctorCheckAgentsSchema runs.
	agentsContent := "---\nagents_schema: \"2.0\"\n---\n\n# GG Agent Instructions\n"
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte(agentsContent), 0o644); err != nil {
		t.Fatalf("write AGENTS.md: %v", err)
	}

	_, _, err := execCmd(t, "doctor")
	// With unreachable services the doctor returns a problem count error.
	// That's fine — we just verify it ran without panicking and covered the
	// doctorCheckAgentsSchema / parseAgentsSchema code paths.
	if err != nil {
		if ee, ok := err.(*ExitError); ok && ee.Code == ExitStoreDown {
			t.Errorf("doctor should not return ExitStoreDown, got: %v", err)
		}
	}
}

func TestDoctor_NoMemgraphURI(t *testing.T) {
	dir := t.TempDir()
	ggDir := filepath.Join(dir, ".gg")
	if err := os.MkdirAll(ggDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(ggDir, "config.yaml"), []byte(configNoMemgraph), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Chdir(dir)

	_, _, err := execCmd(t, "doctor")
	// Expect some problems (Qdrant unreachable) but no panic.
	if err != nil {
		if ee, ok := err.(*ExitError); ok && ee.Code == ExitStoreDown {
			t.Errorf("doctor should not return ExitStoreDown, got: %v", err)
		}
	}
}

func TestDoctor_MissingProjectID(t *testing.T) {
	dir := t.TempDir()
	ggDir := filepath.Join(dir, ".gg")
	if err := os.MkdirAll(ggDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(ggDir, "config.yaml"), []byte(configMissingProjectID), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Chdir(dir)

	// With missing project_id, doctorCheckConfig returns nil and runDoctor
	// takes the "skipped — config invalid" path.
	_, _, err := execCmd(t, "doctor")
	if err != nil {
		if ee, ok := err.(*ExitError); ok && ee.Code == ExitStoreDown {
			t.Errorf("doctor should not return ExitStoreDown, got: %v", err)
		}
	}
}

func TestDoctor_WithIndexStateFile(t *testing.T) {
	ggDir := setupGGDir(t)

	// Create index-state.json to exercise the "present" path in doctorCheckProjectStructure.
	if err := os.WriteFile(filepath.Join(ggDir, "index-state.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatalf("write index-state.json: %v", err)
	}

	_, _, err := execCmd(t, "doctor")
	if err != nil {
		if ee, ok := err.(*ExitError); ok && ee.Code == ExitStoreDown {
			t.Errorf("doctor should not return ExitStoreDown, got: %v", err)
		}
	}
}

func TestDoctor_WithOutboxEntries(t *testing.T) {
	ggDir := setupGGDir(t)

	// Write an outbox entry so doctorCheckOutbox sees a pending entry (fail path).
	payload := map[string]string{"root": "/proj", "lang": "go", "sha": "abc123"}
	if _, err := outbox.Write(ggDir, "full-index", payload); err != nil {
		t.Fatalf("outbox.Write: %v", err)
	}

	_, _, err := execCmd(t, "doctor")
	if err != nil {
		if ee, ok := err.(*ExitError); ok && ee.Code == ExitStoreDown {
			t.Errorf("doctor should not return ExitStoreDown, got: %v", err)
		}
	}
}

func TestIndexerRequiredForProject_LanguageManifests(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/app\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}

	goRequired, err := indexerRequiredForProject(root, nil, indexerSpec{Binary: "scip-go", Lang: "go"})
	if err != nil {
		t.Fatalf("go required: %v", err)
	}
	if !goRequired {
		t.Fatal("go indexer should be required when go.mod exists")
	}

	tsRequired, err := indexerRequiredForProject(root, nil, indexerSpec{Binary: "scip-typescript", Lang: "typescript"})
	if err != nil {
		t.Fatalf("typescript required: %v", err)
	}
	if tsRequired {
		t.Fatal("typescript indexer should be optional when no package.json exists")
	}
}

func TestDoctor_GoOnlyProjectDoesNotFailMissingOptionalIndexers(t *testing.T) {
	report := &doctorReport{}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/app\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}

	for _, spec := range []indexerSpec{
		{Binary: "scip-typescript", Lang: "typescript"},
		{Binary: "scip-python", Lang: "python"},
	} {
		required, err := indexerRequiredForProject(root, nil, spec)
		if err != nil {
			t.Fatalf("%s required: %v", spec.Binary, err)
		}
		if required {
			report.fail(spec.Binary, "unexpected required optional indexer")
		} else {
			report.warn(spec.Binary, "not found — optional")
		}
	}

	if report.problems != 0 {
		t.Fatalf("optional missing indexers should not count as doctor problems, got %d", report.problems)
	}
}

func TestDoctor_AgentsSchema_NoSchemaField(t *testing.T) {
	ggDir := setupGGDir(t)
	root := filepath.Dir(ggDir)

	// AGENTS.md with valid frontmatter but NO agents_schema field —
	// exercises the !ok path in doctorCheckAgentsSchema.
	agentsContent := "---\ntitle: My Project\nauthor: team\n---\n\n# Instructions\n"
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte(agentsContent), 0o644); err != nil {
		t.Fatalf("write AGENTS.md: %v", err)
	}

	_, _, err := execCmd(t, "doctor")
	if err != nil {
		if ee, ok := err.(*ExitError); ok && ee.Code == ExitStoreDown {
			t.Errorf("doctor should not return ExitStoreDown, got: %v", err)
		}
	}
}

func TestDoctor_AgentsSchema_MinorDrift(t *testing.T) {
	ggDir := setupGGDir(t)
	root := filepath.Dir(ggDir)

	// AGENTS.md with same major (2) but higher minor (2.1 vs bundled 2.0) —
	// exercises the "minor drift" warn path in doctorCheckAgentsSchema.
	agentsContent := "---\nagents_schema: \"2.1\"\n---\n\n# GG Agent Instructions\n"
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte(agentsContent), 0o644); err != nil {
		t.Fatalf("write AGENTS.md: %v", err)
	}

	_, _, err := execCmd(t, "doctor")
	if err != nil {
		if ee, ok := err.(*ExitError); ok && ee.Code == ExitStoreDown {
			t.Errorf("doctor should not return ExitStoreDown, got: %v", err)
		}
	}
}

func TestDoctor_AgentsSchema_MajorMismatch(t *testing.T) {
	ggDir := setupGGDir(t)
	root := filepath.Dir(ggDir)

	// AGENTS.md with a different major version (1.x vs bundled 2.x) —
	// exercises the "major version mismatch" fail path.
	agentsContent := "---\nagents_schema: \"1.0\"\n---\n\n# GG Agent Instructions\n"
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte(agentsContent), 0o644); err != nil {
		t.Fatalf("write AGENTS.md: %v", err)
	}

	_, _, err := execCmd(t, "doctor")
	if err != nil {
		if ee, ok := err.(*ExitError); ok && ee.Code == ExitStoreDown {
			t.Errorf("doctor should not return ExitStoreDown, got: %v", err)
		}
	}
}

// ── doctor --reconcile with outbox entries ────────────────────────────────────

func TestDoctor_Reconcile_WithFullIndexEntry(t *testing.T) {
	ggDir := setupGGDir(t)

	// Write an outbox entry simulating a failed index run.
	payload := map[string]string{
		"root": "/tmp/test-project",
		"lang": "go",
		"sha":  "abc12345def67890",
	}
	if _, err := outbox.Write(ggDir, "full-index", payload); err != nil {
		t.Fatalf("outbox.Write: %v", err)
	}

	_, _, err := execCmd(t, "doctor", "--reconcile")
	// Should return an error listing the pending outbox entry.
	if err == nil {
		t.Error("expected error for pending outbox entries, got nil")
	}
}

func TestDoctor_Reconcile_WithChangedIndexEntry(t *testing.T) {
	ggDir := setupGGDir(t)

	payload := map[string]string{
		"root": "/tmp/test-project",
		"lang": "typescript",
		"sha":  "abcdef12",
	}
	if _, err := outbox.Write(ggDir, "changed-index", payload); err != nil {
		t.Fatalf("outbox.Write: %v", err)
	}

	_, _, err := execCmd(t, "doctor", "--reconcile")
	if err == nil {
		t.Error("expected error for pending outbox entries, got nil")
	}
}

func TestDoctor_Reconcile_WithUnknownKindEntry(t *testing.T) {
	ggDir := setupGGDir(t)

	// Write an entry with an unrecognized kind — exercises the default switch case.
	payload := map[string]string{"info": "mystery"}
	if _, err := outbox.Write(ggDir, "mystery-operation", payload); err != nil {
		t.Fatalf("outbox.Write: %v", err)
	}

	_, _, err := execCmd(t, "doctor", "--reconcile")
	if err == nil {
		t.Error("expected error for pending outbox entries, got nil")
	}
}

func TestDoctor_Reconcile_WithRetries(t *testing.T) {
	ggDir := setupGGDir(t)

	// Write then increment retries to exercise the "Retries > 0" branch.
	payload := map[string]string{"root": "/proj", "lang": "go", "sha": "deadbeef"}
	id, err := outbox.Write(ggDir, "full-index", payload)
	if err != nil {
		t.Fatalf("outbox.Write: %v", err)
	}
	if err := outbox.IncrementRetries(ggDir, id); err != nil {
		t.Fatalf("IncrementRetries: %v", err)
	}

	_, _, err = execCmd(t, "doctor", "--reconcile")
	if err == nil {
		t.Error("expected error for pending outbox entries, got nil")
	}
}

func TestDoctor_Reconcile_ShortSHA(t *testing.T) {
	ggDir := setupGGDir(t)

	// SHA shorter than 8 chars — exercises the len(shortSHA) <= 8 path.
	payload := map[string]string{"root": "/proj", "lang": "python", "sha": "abc"}
	if _, err := outbox.Write(ggDir, "full-index", payload); err != nil {
		t.Fatalf("outbox.Write: %v", err)
	}

	_, _, err := execCmd(t, "doctor", "--reconcile")
	if err == nil {
		t.Error("expected error for pending outbox entries, got nil")
	}
}

func TestDoctor_Reconcile_InvalidPayload(t *testing.T) {
	ggDir := setupGGDir(t)

	// Write a raw JSON entry with a full-index kind but an invalid payload
	// so that json.Unmarshal(e.Payload, &p) fails — exercises the else branch.
	outboxDir := filepath.Join(ggDir, "outbox")
	if err := os.MkdirAll(outboxDir, 0o755); err != nil {
		t.Fatalf("mkdir outbox: %v", err)
	}
	// Write a valid Entry JSON but with a payload that can't unmarshal into the
	// expected struct (e.g. an array instead of an object with root/lang/sha).
	rawEntry := `{"id":"test-id-001","kind":"full-index","payload":"not-valid-json-for-struct","created_at":"2026-04-14T12:00:00Z","retries":0}`
	if err := os.WriteFile(filepath.Join(outboxDir, "test-id-001.json"), []byte(rawEntry), 0o644); err != nil {
		t.Fatalf("write entry: %v", err)
	}

	_, _, err := execCmd(t, "doctor", "--reconcile")
	if err == nil {
		t.Error("expected error for pending outbox entries, got nil")
	}
}

// ── parseAgentsSchema direct tests ───────────────────────────────────────────

func TestParseAgentsSchema_Valid(t *testing.T) {
	content := "---\nagents_schema: \"2.0\"\n---\n\n# content"
	schema, ok := parseAgentsSchema(content)
	if !ok {
		t.Fatal("expected ok=true for valid frontmatter")
	}
	if schema != "2.0" {
		t.Errorf("expected schema 2.0, got %q", schema)
	}
}

func TestParseAgentsSchema_NoFrontmatter(t *testing.T) {
	content := "# just a header\nno frontmatter"
	schema, ok := parseAgentsSchema(content)
	if ok || schema != "" {
		t.Errorf("expected (empty, false) for no frontmatter, got (%q, %v)", schema, ok)
	}
}

func TestParseAgentsSchema_NoSchemaField(t *testing.T) {
	content := "---\ntitle: My Project\n---\n\n# content"
	schema, ok := parseAgentsSchema(content)
	if ok || schema != "" {
		t.Errorf("expected (empty, false) for missing agents_schema, got (%q, %v)", schema, ok)
	}
}

func TestParseAgentsSchema_SingleQuoted(t *testing.T) {
	content := "---\nagents_schema: '1.5'\n---\ncontent"
	schema, ok := parseAgentsSchema(content)
	if !ok || schema != "1.5" {
		t.Errorf("expected (1.5, true), got (%q, %v)", schema, ok)
	}
}
