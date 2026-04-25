// Package cmd — fixture-matrix tests for the AC parser embedded in
// .gg/hooks/pre-task-done.d/50-ac-attestation.sh.
//
// Each fixture pair in cmd/testdata/ac_parser/:
//   <name>.txt          — Detail field text fed to the Python parser
//   <name>.expected.json — expected {"count": N, "labels": ["AC-1", ...]}
//
// The test extracts the Python parser snippet from the hook script and runs it
// via python3/python with the fixture content on stdin. This validates the
// parser without invoking gg or git, giving a fast, isolated regression gate.
package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// acParserExpected is the shape of each *.expected.json fixture file.
type acParserExpected struct {
	Count  int      `json:"count"`
	Labels []string `json:"labels"`
}

// extractPythonParser pulls the Python AC-parser snippet out of the hook
// script. It returns the content of the ACS=$(...) Python -c block:
//
//	ACS=$(printf '%s' "$DETAIL" | $PY -c "  ← start marker
//	  ... python code ...
//	" 2>/dev/null || true)                   ← end marker
func extractPythonParser(t *testing.T, hookPath string) string {
	t.Helper()
	raw, err := os.ReadFile(hookPath)
	if err != nil {
		t.Fatalf("read hook: %v", err)
	}

	// Find the ACS= block by scanning lines. We want the *second* $PY -c "
	// occurrence (first is the DETAIL= block which just does json.load).
	lines := strings.Split(string(raw), "\n")
	startMarker := `$PY -c "`
	endMarker := `" 2>/dev/null || true)`

	var starts []int
	for i, l := range lines {
		if strings.Contains(l, startMarker) {
			starts = append(starts, i)
		}
	}
	if len(starts) < 2 {
		t.Fatalf("expected at least 2 '$PY -c \"' blocks in hook, found %d", len(starts))
	}
	// Second block is the ACS parser (index 1).
	blockStart := starts[1] + 1 // line after the opening marker
	for i := blockStart; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == strings.TrimSpace(endMarker) {
			return strings.Join(lines[blockStart:i], "\n")
		}
	}
	t.Fatal("could not find end marker for Python ACS parser block")
	return ""
}

// runPythonParser runs the extracted Python snippet with detail as stdin and
// returns the tab-separated output lines.
func runPythonParser(t *testing.T, pyCode, detail string) []string {
	t.Helper()

	py := "python3"
	if _, err := exec.LookPath(py); err != nil {
		py = "python"
		if _, err2 := exec.LookPath(py); err2 != nil {
			t.Skip("python3/python not available — skipping AC parser fixture tests")
		}
	}

	cmd := exec.Command(py, "-c", pyCode)
	cmd.Stdin = strings.NewReader(detail)
	var out bytes.Buffer
	var errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		t.Fatalf("python parser failed: %v\nstderr: %s", err, errBuf.String())
	}
	raw := strings.TrimRight(out.String(), "\n")
	if raw == "" {
		return nil
	}
	return strings.Split(raw, "\n")
}

// labelsFromOutput converts parser output lines (tab-separated num\ttext\tgap)
// into sequential AC-N labels ("AC-1", "AC-2", …).
func labelsFromOutput(lines []string) []string {
	labels := make([]string, 0, len(lines))
	for i, line := range lines {
		if line == "" {
			continue
		}
		_ = line // text and gap_label not needed for label derivation
		labels = append(labels, "AC-"+string(rune('0'+i+1)))
	}
	return labels
}

// TestACParserFixtures runs the Python AC parser against every fixture pair
// in cmd/testdata/ac_parser/ and asserts the output matches the expected JSON.
func TestACParserFixtures(t *testing.T) {
	// Locate hook via runtime.Caller so tests work from any working directory.
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(thisFile))
	hookPath := filepath.Join(repoRoot, ".gg", "hooks", "pre-task-done.d", "50-ac-attestation.sh")
	if _, err := os.Stat(hookPath); err != nil {
		t.Skipf("hook not found at %s — skipping: %v", hookPath, err)
	}

	pyCode := extractPythonParser(t, hookPath)

	fixtureDir := filepath.Join(repoRoot, "cmd", "testdata", "ac_parser")
	entries, err := os.ReadDir(fixtureDir)
	if err != nil {
		t.Fatalf("read fixture dir: %v", err)
	}

	type fixture struct {
		name     string
		txtPath  string
		jsonPath string
	}

	var fixtures []fixture
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".txt") {
			continue
		}
		base := strings.TrimSuffix(e.Name(), ".txt")
		jsonPath := filepath.Join(fixtureDir, base+".expected.json")
		if _, err := os.Stat(jsonPath); err != nil {
			t.Errorf("fixture %s.txt has no matching .expected.json", base)
			continue
		}
		fixtures = append(fixtures, fixture{
			name:     base,
			txtPath:  filepath.Join(fixtureDir, e.Name()),
			jsonPath: jsonPath,
		})
	}

	if len(fixtures) == 0 {
		t.Fatal("no fixtures found in cmd/testdata/ac_parser/")
	}

	for _, fx := range fixtures {
		fx := fx
		t.Run(fx.name, func(t *testing.T) {
			// Load fixture detail text.
			detailBytes, err := os.ReadFile(fx.txtPath)
			if err != nil {
				t.Fatalf("read %s: %v", fx.txtPath, err)
			}

			// Load expected output.
			jsonBytes, err := os.ReadFile(fx.jsonPath)
			if err != nil {
				t.Fatalf("read %s: %v", fx.jsonPath, err)
			}
			var want acParserExpected
			if err := json.Unmarshal(jsonBytes, &want); err != nil {
				t.Fatalf("parse %s: %v", fx.jsonPath, err)
			}

			// Run parser.
			outLines := runPythonParser(t, pyCode, string(detailBytes))
			gotCount := len(outLines)
			gotLabels := labelsFromOutput(outLines)
			if gotLabels == nil {
				gotLabels = []string{}
			}

			// Assert count.
			if gotCount != want.Count {
				t.Errorf("count: got %d, want %d", gotCount, want.Count)
				t.Logf("parser output:\n%s", strings.Join(outLines, "\n"))
			}

			// Assert labels.
			if len(gotLabels) != len(want.Labels) {
				t.Errorf("labels length: got %v, want %v", gotLabels, want.Labels)
			} else {
				for i, wl := range want.Labels {
					if i >= len(gotLabels) {
						break
					}
					if gotLabels[i] != wl {
						t.Errorf("label[%d]: got %q, want %q", i, gotLabels[i], wl)
					}
				}
			}
		})
	}
}
