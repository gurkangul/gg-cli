package adapters_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gurkangul/gg-cli/internal/adapters"
)

// agentsContent is a minimal representative AGENTS.md snippet used as input
// for all golden-file adapter tests.
const agentsContent = `## SESSION START
Run gg status at the start of every session.

## DECISION POINT
When a decision is reached: gg record "text" --reason "why"
`

// TestAdapterGoldenFiles verifies that each adapter produces the expected
// output format when injecting agentsContent into a fresh file.
func TestAdapterGoldenFiles(t *testing.T) {
	cases := []struct {
		adapterName string
		targetFile  string
		checkFuncs  []func(t *testing.T, body string)
	}{
		{
			adapterName: "cursor",
			targetFile:  ".cursorrules",
			checkFuncs: []func(t *testing.T, body string){
				hasMarkers,
				func(t *testing.T, body string) {
					t.Helper()
					if !strings.Contains(body, "SESSION START") {
						t.Error("cursor: AGENTS.md content not injected")
					}
				},
			},
		},
		{
			adapterName: "aider",
			targetFile:  "CONVENTIONS.md",
			checkFuncs: []func(t *testing.T, body string){
				hasMarkers,
				func(t *testing.T, body string) {
					t.Helper()
					if !strings.Contains(body, "DECISION POINT") {
						t.Error("aider: AGENTS.md content not injected")
					}
				},
			},
		},
		{
			adapterName: "cline",
			targetFile:  ".clinerules",
			checkFuncs:  []func(t *testing.T, body string){hasMarkers},
		},
		{
			adapterName: "windsurf",
			targetFile:  ".windsurfrules",
			checkFuncs:  []func(t *testing.T, body string){hasMarkers},
		},
		{
			adapterName: "roo",
			targetFile:  ".roorules",
			checkFuncs:  []func(t *testing.T, body string){hasMarkers},
		},
		{
			adapterName: "claude-code",
			targetFile:  "CLAUDE.md",
			checkFuncs:  []func(t *testing.T, body string){hasMarkers},
		},
		{
			adapterName: "continue",
			targetFile:  ".continue/config.json",
			checkFuncs: []func(t *testing.T, body string){
				// continue uses ManagedBlock — markers should be present.
				hasMarkers,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.adapterName, func(t *testing.T) {
			a := adapters.ByName(tc.adapterName)
			if a == nil {
				t.Fatalf("adapter %q not found in registry", tc.adapterName)
			}

			dir := t.TempDir()
			targetPath := filepath.Join(dir, tc.targetFile)

			result, err := adapters.Inject(a, targetPath, agentsContent)
			if err != nil {
				t.Fatalf("Inject: %v", err)
			}
			if !result.Created {
				t.Error("expected Created=true for fresh file")
			}

			data, err := os.ReadFile(targetPath)
			if err != nil {
				t.Fatalf("read target: %v", err)
			}
			body := string(data)

			for _, fn := range tc.checkFuncs {
				fn(t, body)
			}

			// Check reports up-to-date immediately after inject.
			upToDate, drift, err := adapters.Check(a, targetPath, agentsContent)
			if err != nil {
				t.Fatalf("Check: %v", err)
			}
			if !upToDate {
				t.Errorf("Check reports drift immediately after inject: %s", drift)
			}
		})
	}
}

// TestNativeAgentsNoAdaptNeeded documents that codex is "native" (reads
// AGENTS.md directly) and the adapter correctly detects an existing file.
func TestNativeAgentsNoAdaptNeeded(t *testing.T) {
	a := adapters.ByName("codex")
	if a == nil {
		t.Fatal("codex adapter not in registry")
	}

	dir := t.TempDir()
	agentsPath := filepath.Join(dir, "AGENTS.md")
	if err := os.WriteFile(agentsPath, []byte(agentsContent), 0o644); err != nil {
		t.Fatal(err)
	}

	targetPath := a.ResolveTarget(dir)
	upToDate, _, err := adapters.Check(a, targetPath, agentsContent)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !upToDate {
		t.Error("codex adapter should report up-to-date when AGENTS.md exists")
	}
}

// TestRegistryCompleteness ensures every adapter has at least one target file.
func TestRegistryCompleteness(t *testing.T) {
	for _, a := range adapters.All() {
		if len(a.Targets) == 0 {
			t.Errorf("adapter %q has no target files defined", a.Name)
		}
		if a.Role == "" {
			t.Errorf("adapter %q has no role defined", a.Name)
		}
	}
}

// TestDetect verifies that Detect returns the correct adapters based on
// the files present in the project directory.
func TestDetect(t *testing.T) {
	dir := t.TempDir()

	// Simulate a project with cursor and aider config.
	for _, f := range []string{".cursorrules", ".aider.conf.yml"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte{}, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	detected := adapters.Detect(dir)
	found := map[string]bool{}
	for _, a := range detected {
		found[a.Name] = true
	}

	if !found["cursor"] {
		t.Error("cursor not detected despite .cursorrules present")
	}
	if !found["aider"] {
		t.Error("aider not detected despite .aider.conf.yml present")
	}
	if found["windsurf"] {
		t.Error("windsurf incorrectly detected")
	}
}

// hasMarkers is a reusable check that the managed-block delimiters are present.
func hasMarkers(t *testing.T, body string) {
	t.Helper()
	if !strings.Contains(body, "<!-- gg-managed-start -->") {
		t.Error("managed-start marker missing")
	}
	if !strings.Contains(body, "<!-- gg-managed-end -->") {
		t.Error("managed-end marker missing")
	}
}
