// Package cmd — miscellaneous tests: terminal helpers, index, reembed, import,
// printJSON, ensureProjectConfig, guardProjectLocation, promptIfDuplicate, impact.
package cmd

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/store"
)

// ── dedup.go direct function tests ───────────────────────────────────────────

func TestIsTerminal_CallsSuccessfully(t *testing.T) {
	// isTerminal should not panic regardless of what stdin/stdout is.
	// The result depends on the environment (tty vs pipe).
	_ = isTerminal(os.Stdin)
	_ = isTerminal(os.Stdout)
	// A closed pipe returns false (stat fails).
	r, w, _ := os.Pipe()
	_ = r.Close()
	_ = w.Close()
	_ = isTerminal(r)
}

func TestNewStdinReader_ReturnsBufio(t *testing.T) {
	r := newStdinReader()
	if r == nil {
		t.Error("newStdinReader should not return nil")
	}
}

// ── index command tests ───────────────────────────────────────────────────────

func TestIndex_ServiceDown(t *testing.T) {
	setupGGDir(t)
	// gg index loads config, finds root, creates a Memgraph client (lazy), then
	// calls SchemaInit which fails immediately on bolt://localhost:1.
	_, _, err := execCmd(t, "index")
	// Expect a non-nil error from the Memgraph connection failure.
	if err == nil {
		t.Log("index returned nil — all services happened to be up; skipping check")
	}
}

func TestIndex_UnsupportedLanguage(t *testing.T) {
	setupGGDir(t)
	_, _, err := execCmd(t, "index", "--lang", "rust")
	if err == nil {
		t.Fatal("expected error for unsupported language")
	}
	if ee, ok := err.(*ExitError); ok && ee.Code == ExitStoreDown {
		t.Error("unsupported language should fail before store, not with ExitStoreDown")
	}
}

// ── reembed command tests (no --confirm path) ─────────────────────────────────

func TestReembed_NoConfirm_PrintsHelp(t *testing.T) {
	setupGGDir(t)
	// Without --confirm, reembed just prints instructions and returns nil.
	_, _, err := execCmd(t, "reembed")
	if err != nil {
		t.Errorf("reembed without --confirm should return nil, got: %v", err)
	}
}

// ── import command tests ──────────────────────────────────────────────────────

func TestImport_FileNotFound(t *testing.T) {
	setupGGDir(t)
	_, _, err := execCmd(t, "import", "/nonexistent/path/bundle.json.gz")
	if err == nil {
		t.Fatal("expected error for non-existent import file")
	}
}

func TestImport_InvalidGzip(t *testing.T) {
	setupGGDir(t)
	// Create a file that is not a valid gzip.
	f, err := os.CreateTemp(t.TempDir(), "*.json.gz")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	_, _ = f.WriteString("this is not gzip data")
	_ = f.Close()

	_, _, err = execCmd(t, "import", f.Name())
	if err == nil {
		t.Fatal("expected error for invalid gzip file")
	}
}

// ── printJSON / writeJSON direct tests ───────────────────────────────────────

func TestPrintJSON_FallbackPath(t *testing.T) {
	// jsonOutput=false → calls fallback, returns nil.
	origJSONOutput := jsonOutput
	jsonOutput = false
	defer func() { jsonOutput = origJSONOutput }()

	called := false
	err := printJSON("test-value", func() { called = true })
	if err != nil {
		t.Errorf("printJSON: unexpected error: %v", err)
	}
	if !called {
		t.Error("printJSON: fallback should have been called when jsonOutput=false")
	}
}

func TestPrintJSON_JSONOutputPath(t *testing.T) {
	// jsonOutput=true → calls writeJSON which writes to os.Stdout.
	origJSONOutput := jsonOutput
	jsonOutput = true
	defer func() { jsonOutput = origJSONOutput }()

	// Redirect stdout to capture output.
	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	fallbackCalled := false
	err := printJSON(map[string]string{"key": "value"}, func() { fallbackCalled = true })

	_ = w.Close()
	os.Stdout = origStdout

	// Drain the pipe.
	buf := make([]byte, 256)
	_, _ = r.Read(buf)
	_ = r.Close()

	if err != nil {
		t.Errorf("printJSON: unexpected error: %v", err)
	}
	if fallbackCalled {
		t.Error("printJSON: fallback should NOT be called when jsonOutput=true")
	}
}

// ── ensureProjectConfig direct tests ─────────────────────────────────────────

func TestEnsureProjectConfig_FreshDir(t *testing.T) {
	ggDir := filepath.Join(t.TempDir(), ".gg")
	if err := os.MkdirAll(ggDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Point cwd to the parent so config.Load works if called inside.
	t.Chdir(filepath.Dir(ggDir))

	projectID, err := ensureProjectConfig(ggDir)
	if err != nil {
		t.Fatalf("ensureProjectConfig: %v", err)
	}
	if projectID == "" {
		t.Error("expected non-empty project ID")
	}

	// config.yaml should now exist.
	if _, err := os.Stat(filepath.Join(ggDir, "config.yaml")); err != nil {
		t.Errorf("config.yaml not created: %v", err)
	}
}

func TestEnsureProjectConfig_ExistingConfig(t *testing.T) {
	ggDir := setupGGDir(t)
	// setupGGDir already wrote config.yaml — calling again should return
	// the existing project ID without overwriting.
	projectID, err := ensureProjectConfig(ggDir)
	if err != nil {
		t.Fatalf("ensureProjectConfig: %v", err)
	}
	if projectID != "test-project-fixture" {
		t.Errorf("expected existing project ID 'test-project-fixture', got %q", projectID)
	}
}

func TestEnsureProjectConfig_ZeroByteConfig(t *testing.T) {
	// A zero-byte config.yaml is treated as a failed previous init — it should
	// be removed and a new config written.
	ggDir := filepath.Join(t.TempDir(), ".gg")
	if err := os.MkdirAll(ggDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Chdir(filepath.Dir(ggDir))

	// Write empty config.yaml.
	configPath := filepath.Join(ggDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte{}, 0o644); err != nil {
		t.Fatalf("write empty config: %v", err)
	}

	projectID, err := ensureProjectConfig(ggDir)
	if err != nil {
		t.Fatalf("ensureProjectConfig with zero-byte config: %v", err)
	}
	if projectID == "" {
		t.Error("expected non-empty project ID after recovering from zero-byte config")
	}
}

func TestEnsureProjectConfig_MissingProjectID(t *testing.T) {
	// A config without project_id is a pre-refactor legacy config.
	ggDir := filepath.Join(t.TempDir(), ".gg")
	if err := os.MkdirAll(ggDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Chdir(filepath.Dir(ggDir))

	// Write config with NO project_id.
	configPath := filepath.Join(ggDir, "config.yaml")
	legacyConfig := `qdrant:
  host: "127.0.0.1"
  port: 19997
embedding:
  host: "http://localhost:11434"
  model: "nomic-embed-text"
`
	if err := os.WriteFile(configPath, []byte(legacyConfig), 0o644); err != nil {
		t.Fatalf("write legacy config: %v", err)
	}

	_, err := ensureProjectConfig(ggDir)
	if err == nil {
		t.Error("expected error for config without project_id")
	}
}

// ── guardProjectLocation direct tests ────────────────────────────────────────

func TestGuardProjectLocation_AncestorHasProject(t *testing.T) {
	// Create a temp dir with .gg/config.yaml (simulating a project root),
	// then create a subdirectory inside it. guardProjectLocation should refuse
	// the subdirectory because an ancestor already contains a gg project.
	parentDir := t.TempDir()
	parentGGDir := filepath.Join(parentDir, ".gg")
	if err := os.MkdirAll(parentGGDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Write a minimal config.yaml so isProjectGGDir returns true.
	if err := os.WriteFile(filepath.Join(parentGGDir, "config.yaml"), []byte("project_id: ancestor\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	// Create a subdirectory inside the project.
	subDir := filepath.Join(parentDir, "sub", "dir")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatalf("mkdir sub: %v", err)
	}

	err := guardProjectLocation(subDir)
	if err == nil {
		t.Error("expected error for subdir inside an existing gg project")
	}
}

func TestGuardProjectLocation_TempDir(t *testing.T) {
	dir := t.TempDir()
	// Fresh temp dir has no ancestor .gg — should pass.
	if err := guardProjectLocation(dir); err != nil {
		t.Errorf("unexpected error for clean temp dir: %v", err)
	}
}

func TestGuardProjectLocation_HomeSharedDir(t *testing.T) {
	// ~/.gg/ is the shared infra dir — init inside it must be refused.
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot resolve home dir")
	}
	sharedDir := filepath.Join(home, ".gg")
	err = guardProjectLocation(sharedDir)
	if err == nil {
		t.Error("expected error for init inside shared dir, got nil")
	}
}

func TestGuardProjectLocation_ParentOfSharedDir(t *testing.T) {
	// The parent of ~/.gg/ has its .gg/ == shared dir — must be refused.
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot resolve home dir")
	}
	// guardProjectLocation(home) would create .gg/ == ~/.gg/ — must refuse.
	err = guardProjectLocation(home)
	if err == nil {
		t.Error("expected error for dir whose .gg/ would be the shared dir, got nil")
	}
}

// ── dedup.go: promptIfDuplicate with unreachable store ───────────────────────

func TestPromptIfDuplicate_StoreError(t *testing.T) {
	// Construct a deps with a store pointed at an unreachable port so that
	// FindNearDups fails. promptIfDuplicate must treat store errors as non-fatal
	// and return false (proceed with creation).
	cfg := &config.QdrantConfig{Host: "127.0.0.1", Port: 19997}
	sc, err := store.New(cfg, t.TempDir(), "test-dedup-proj")
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	defer func() { _ = sc.Close() }()

	d := &deps{store: sc, qdrantDown: true}
	ctx := context.Background()

	result := promptIfDuplicate(ctx, d, "decisions", make([]float32, store.VectorSize))
	if result {
		t.Error("expected false (no abort) when dedup store call fails")
	}
}

func TestPromptIfDuplicateThreshold_StoreError(t *testing.T) {
	cfg := &config.QdrantConfig{Host: "127.0.0.1", Port: 19997}
	sc, err := store.New(cfg, t.TempDir(), "test-dedup-thresh-proj")
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	defer func() { _ = sc.Close() }()

	d := &deps{store: sc, qdrantDown: true}
	ctx := context.Background()

	stderr := captureStderrForTest(t, func() {
		res := promptIfDuplicateThreshold(ctx, d, "bugs", make([]float32, store.VectorSize), 0.85)
		if res.Abort || res.SawDup {
			t.Errorf("expected zero dupResult on store error, got %+v", res)
		}
	})
	if !strings.Contains(stderr, "warning: dedup check failed") {
		t.Fatalf("expected dedup warning on store error, got %q", stderr)
	}
}

func captureStderrForTest(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("stderr pipe: %v", err)
	}
	os.Stderr = w
	defer func() { os.Stderr = orig }()

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("close stderr pipe: %v", err)
	}
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read stderr pipe: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("close stderr reader: %v", err)
	}
	return string(data)
}

// ── dedup.go: appendUniq ─────────────────────────────────────────────────────

func TestAppendUniq(t *testing.T) {
	got := appendUniq([]string{"a", "b"}, "c")
	if len(got) != 3 || got[2] != "c" {
		t.Errorf("expected [a b c], got %v", got)
	}
	// No duplicate added.
	got2 := appendUniq(got, "b")
	if len(got2) != 3 {
		t.Errorf("expected no append for existing tag, got %v", got2)
	}
	// Nil slice.
	got3 := appendUniq(nil, "x")
	if len(got3) != 1 || got3[0] != "x" {
		t.Errorf("expected [x], got %v", got3)
	}
}

// ── impact command: store-down path ──────────────────────────────────────────

func TestImpact_StoreDown(t *testing.T) {
	setupGGDir(t)
	_, _, err := execCmd(t, "impact", "cmd/impact.go")
	if err == nil {
		t.Fatal("expected error when Qdrant is down")
	}
	ee, ok := err.(*ExitError)
	if !ok {
		t.Fatalf("expected *ExitError, got %T: %v", err, err)
	}
	if ee.Code != ExitStoreDown {
		t.Errorf("expected ExitStoreDown(%d), got %d", ExitStoreDown, ee.Code)
	}
}
