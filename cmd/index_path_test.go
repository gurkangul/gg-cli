package cmd

import (
	"path/filepath"
	"testing"
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
			got, ok := normalizeProjectPath(root, tc.raw)
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
