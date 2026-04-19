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

func TestDiscoverModuleDirs_UnsupportedLang(t *testing.T) {
	_, err := discoverModuleDirs(t.TempDir(), runner.Lang("ruby"))
	if err == nil {
		t.Fatal("expected error for unsupported lang")
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil { //nolint:gosec
		t.Fatalf("writeFile %s: %v", path, err)
	}
}
