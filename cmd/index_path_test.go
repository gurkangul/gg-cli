package cmd

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/gurkangul/gg-cli/internal/index/runner"
)

// TestNormalizeProjectPath covers BUG-010 (absolute paths) and BUG-011
// (Go build-cache paths escaping project root via ../..).
func TestNormalizeProjectPath(t *testing.T) {
	root := "/Users/alice/projects/gg-cli"

	cases := []struct {
		name    string
		raw     string
		wantRel string
		wantOK  bool
	}{
		// In-tree paths: normalise to forward-slash relative.
		{"relative simple", "cmd/brain.go", "cmd/brain.go", true},
		{"relative nested", "internal/store/brain_export.go", "internal/store/brain_export.go", true},
		{"absolute in-tree", "/Users/alice/projects/gg-cli/cmd/brain.go", "cmd/brain.go", true},
		{"redundant dots", "cmd/./brain.go", "cmd/brain.go", true},
		{"trailing dot-segments", "cmd/foo/../brain.go", "cmd/brain.go", true},

		// Out-of-tree — must be rejected.
		{"escape via dotdot", "../../Library/Caches/go-build/02/abc.go", "", false},
		{"absolute outside root", "/Users/alice/other/project/file.go", "", false},
		{"absolute Library cache", "/Users/alice/Library/Caches/go-build/x.go", "", false},
		{"empty path", "", "", false},
		{"dot (root itself)", ".", "", false},
		{"root absolute", "/Users/alice/projects/gg-cli", "", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := normalizeProjectPath(root, "", tc.raw)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v (got path=%q)", ok, tc.wantOK, got)
			}
			if got != tc.wantRel {
				t.Fatalf("rel = %q, want %q", got, tc.wantRel)
			}
			if ok {
				if filepath.IsAbs(got) {
					t.Errorf("returned abs path %q — must be relative", got)
				}
				if len(got) >= 2 && got[:2] == ".." {
					t.Errorf("returned escape path %q — must stay inside root", got)
				}
			}
		})
	}
}

// TestNormalizeProjectPath_Monorepo covers TASK-214: scip-go runs in a
// subdirectory (module root), and the emitted relative paths must resolve to
// project-root-relative paths for storage.
func TestNormalizeProjectPath_Monorepo(t *testing.T) {
	root := "/Users/alice/projects/onelift"
	moduleDir := "/Users/alice/projects/onelift/lift-cli"

	cases := []struct {
		name    string
		raw     string
		wantRel string
		wantOK  bool
	}{
		// scip-go emits paths relative to its cwd — which is the module dir.
		{"relative module file", "cmd/main.go", "lift-cli/cmd/main.go", true},
		{"relative nested", "internal/store/foo.go", "lift-cli/internal/store/foo.go", true},
		// Absolute paths bypass baseDir and resolve directly.
		{"absolute in project", "/Users/alice/projects/onelift/lift-cli/cmd/main.go", "lift-cli/cmd/main.go", true},
		// Out-of-tree absolute paths still rejected.
		{"absolute out-of-tree", "/Users/alice/Library/Caches/go-build/x.go", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := normalizeProjectPath(root, moduleDir, tc.raw)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v (got path=%q)", ok, tc.wantOK, got)
			}
			if got != tc.wantRel {
				t.Fatalf("rel = %q, want %q", got, tc.wantRel)
			}
		})
	}
}

func TestStripModulePrefix(t *testing.T) {
	cases := []struct {
		name          string
		projectRel    string
		moduleRelRoot string
		want          string
	}{
		{"root module no-op", "cmd/foo.go", ".", "cmd/foo.go"},
		{"empty root no-op", "cmd/foo.go", "", "cmd/foo.go"},
		{"sub module strip", "lift-cli/cmd/foo.go", "lift-cli", "cmd/foo.go"},
		{"nested module strip", "packages/api/src/main.ts", "packages/api", "src/main.ts"},
		{"path not under module", "other/file.go", "lift-cli", "other/file.go"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := stripModulePrefix(tc.projectRel, tc.moduleRelRoot)
			if got != tc.want {
				t.Fatalf("stripModulePrefix(%q, %q) = %q, want %q", tc.projectRel, tc.moduleRelRoot, got, tc.want)
			}
		})
	}
}

func TestDiscoverModuleDirs_GoModAtRoot(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/root\n")
	// Even if a subdirectory has go.mod, root-level short-circuits.
	sub := filepath.Join(root, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(sub, "go.mod"), "module example.com/sub\n")

	dirs, err := discoverModuleDirs(root, runner.LangGo)
	if err != nil {
		t.Fatalf("discoverModuleDirs: %v", err)
	}
	if len(dirs) != 1 || dirs[0] != root {
		t.Fatalf("expected [%q], got %v", root, dirs)
	}
}

func TestDiscoverModuleDirs_GoModInSubdirs(t *testing.T) {
	root := t.TempDir()
	// No go.mod at root — two sub-modules (the monorepo case TASK-214 targets).
	for _, sub := range []string{"lift-cli", "services/api"} {
		dir := filepath.Join(root, sub)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/"+sub+"\n")
	}

	dirs, err := discoverModuleDirs(root, runner.LangGo)
	if err != nil {
		t.Fatalf("discoverModuleDirs: %v", err)
	}
	if len(dirs) != 2 {
		t.Fatalf("expected 2 module dirs, got %d: %v", len(dirs), dirs)
	}
	sort.Strings(dirs)
	want := []string{filepath.Join(root, "lift-cli"), filepath.Join(root, "services/api")}
	sort.Strings(want)
	for i, w := range want {
		if dirs[i] != w {
			t.Fatalf("dirs[%d] = %q, want %q", i, dirs[i], w)
		}
	}
}

func TestDiscoverModuleDirs_NoManifest(t *testing.T) {
	root := t.TempDir()
	dirs, err := discoverModuleDirs(root, runner.LangGo)
	if err != nil {
		t.Fatalf("discoverModuleDirs: %v", err)
	}
	if len(dirs) != 0 {
		t.Fatalf("expected no dirs for empty project, got %v", dirs)
	}
}

func TestDiscoverModuleDirs_TypeScriptTsconfigAtRoot(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "tsconfig.json"), "{}\n")

	dirs, err := discoverModuleDirs(root, runner.LangTypeScript)
	if err != nil {
		t.Fatalf("discoverModuleDirs: %v", err)
	}
	if len(dirs) != 1 || dirs[0] != root {
		t.Fatalf("expected [%q], got %v", root, dirs)
	}
}

func TestDiscoverModuleDirs_SourceDirFallback(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "src", "index.ts"), "export const value = 1\n")

	dirs, err := discoverModuleDirs(root, runner.LangTypeScript)
	if err != nil {
		t.Fatalf("discoverModuleDirs: %v", err)
	}
	if len(dirs) != 1 || dirs[0] != root {
		t.Fatalf("expected [%q], got %v", root, dirs)
	}
}

func TestDiscoverModuleDirs_SourceDirFallbackDoesNotOverrideNestedModules(t *testing.T) {
	root := t.TempDir()
	for _, sub := range []string{"services/api", "packages/web"} {
		dir := filepath.Join(root, sub)
		if err := os.MkdirAll(filepath.Join(dir, "src"), 0o755); err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(dir, "package.json"), "{\"name\":\""+sub+"\"}\n")
		writeFile(t, filepath.Join(dir, "src", "index.ts"), "export const value = 1\n")
	}

	dirs, err := discoverModuleDirs(root, runner.LangTypeScript)
	if err != nil {
		t.Fatalf("discoverModuleDirs: %v", err)
	}
	if len(dirs) != 2 {
		t.Fatalf("expected 2 nested dirs, got %v", dirs)
	}
	sort.Strings(dirs)
	want := []string{filepath.Join(root, "packages/web"), filepath.Join(root, "services/api")}
	sort.Strings(want)
	for i, w := range want {
		if dirs[i] != w {
			t.Fatalf("dirs[%d] = %q, want %q", i, dirs[i], w)
		}
	}
}

func TestDiscoverModuleDirs_UnsupportedLang(t *testing.T) {
	_, err := discoverModuleDirs(t.TempDir(), runner.Lang("ruby"))
	if err == nil {
		t.Fatal("expected error for unsupported lang")
	}
}

// TestDiscoverModuleDirs_PythonRequirementsTxt verifies that a project with
// only requirements.txt (no pyproject.toml) is still discovered — the storygift
// baseline that motivated multi-manifest support (TASK-270).
func TestDiscoverModuleDirs_PythonRequirementsTxt(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "requirements.txt"), "requests==2.31.0\n")

	dirs, err := discoverModuleDirs(root, runner.LangPython)
	if err != nil {
		t.Fatalf("discoverModuleDirs: %v", err)
	}
	if len(dirs) != 1 || dirs[0] != root {
		t.Fatalf("expected [%q], got %v", root, dirs)
	}
}

// TestDiscoverModuleDirs_PythonPyprojectPreferredOverRequirements verifies
// that pyproject.toml wins when both are present in the same directory —
// priority order is pyproject.toml > setup.py > setup.cfg > Pipfile > requirements.txt.
func TestDiscoverModuleDirs_PythonPyprojectPreferredOverRequirements(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "pyproject.toml"), "[build-system]\n")
	writeFile(t, filepath.Join(root, "requirements.txt"), "requests==2.31.0\n")

	dirs, err := discoverModuleDirs(root, runner.LangPython)
	if err != nil {
		t.Fatalf("discoverModuleDirs: %v", err)
	}
	// Root fast-path fires on pyproject.toml first — single result expected.
	if len(dirs) != 1 || dirs[0] != root {
		t.Fatalf("expected [%q], got %v", root, dirs)
	}
}

// TestDiscoverModuleDirs_PythonDedup verifies that a subdir containing both
// requirements.txt and setup.py is counted once, not twice.
func TestDiscoverModuleDirs_PythonDedup(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "app")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(sub, "requirements.txt"), "flask\n")
	writeFile(t, filepath.Join(sub, "setup.py"), "from setuptools import setup; setup()\n")

	dirs, err := discoverModuleDirs(root, runner.LangPython)
	if err != nil {
		t.Fatalf("discoverModuleDirs: %v", err)
	}
	if len(dirs) != 1 || dirs[0] != sub {
		t.Fatalf("expected [%q], got %v", sub, dirs)
	}
}

// TestManifestsForLang verifies the manifest list for each supported language.
func TestManifestsForLang(t *testing.T) {
	cases := []struct {
		lang runner.Lang
		want []string
	}{
		{runner.LangGo, []string{"go.mod"}},
		{runner.LangTypeScript, []string{"package.json", "tsconfig.json", "jsconfig.json"}},
		{runner.LangPython, []string{"pyproject.toml", "setup.py", "setup.cfg", "Pipfile", "requirements.txt"}},
		{runner.Lang("ruby"), nil},
	}
	for _, tc := range cases {
		got := manifestsForLang(tc.lang)
		if len(got) != len(tc.want) {
			t.Errorf("manifestsForLang(%q): got %v, want %v", tc.lang, got, tc.want)
			continue
		}
		for i, w := range tc.want {
			if got[i] != w {
				t.Errorf("manifestsForLang(%q)[%d] = %q, want %q", tc.lang, i, got[i], w)
			}
		}
	}
}

func TestManifestsForLang_ExcludesDependencyLockfiles(t *testing.T) {
	lockfiles := map[runner.Lang][]string{
		runner.LangGo:         {"go.sum"},
		runner.LangTypeScript: {"package-lock.json", "pnpm-lock.yaml", "yarn.lock"},
		runner.LangPython:     {"poetry.lock", "uv.lock", "Pipfile.lock"},
	}
	for lang, excluded := range lockfiles {
		got := manifestsForLang(lang)
		for _, name := range excluded {
			if stringSliceContains(got, name) {
				t.Fatalf("manifestsForLang(%q) unexpectedly includes lockfile %q in %v", lang, name, got)
			}
		}
	}
}

func stringSliceContains(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil { //nolint:gosec
		t.Fatalf("writeFile %s: %v", path, err)
	}
}
