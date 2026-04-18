package cmd

import "testing"

// TestDerivePackagePath guards BUG-014: Package nodes were never populated
// because derivePackagePath (and the call to upsertPackage in OnFile) did not
// exist before the fix. This test verifies that derivePackagePath returns a
// non-empty path for Go files, which is the trigger condition for package node
// creation in graphHandler.OnFile.
func TestDerivePackagePath(t *testing.T) {
	cases := []struct {
		name       string
		modulePath string
		lang       string
		relFile    string
		want       string
	}{
		{"go nested", "github.com/gurkangul/gg-cli", "go", "internal/store/bugs.go", "github.com/gurkangul/gg-cli/internal/store"},
		{"go root", "github.com/gurkangul/gg-cli", "go", "main.go", "github.com/gurkangul/gg-cli"},
		{"go no module", "", "go", "cmd/foo.go", "cmd"},
		{"ts nested", "", "typescript", "src/components/Button.ts", "src/components"},
		{"ts root", "", "typescript", "index.ts", "."},
		{"python nested", "", "python", "pkg/util/helpers.py", "pkg/util"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := derivePackagePath(tc.modulePath, tc.lang, tc.relFile)
			if got != tc.want {
				t.Errorf("derivePackagePath(%q, %q, %q) = %q; want %q",
					tc.modulePath, tc.lang, tc.relFile, got, tc.want)
			}
			if got == "" {
				t.Error("returned empty path — package node would not be created (regression of BUG-014)")
			}
		})
	}
}
