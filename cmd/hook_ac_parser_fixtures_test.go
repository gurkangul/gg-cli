// Package cmd — fixture-matrix tests for the AC parser embedded in
// .gg/hooks/pre-task-done.d/50-ac-attestation.sh.
//
// Each fixture pair in cmd/testdata/ac_parser/:
//
//	<name>.txt           — Detail field text fed to the Python parser
//	<name>.expected.json — expected {"count": N, "entries": [{"num": N, "text_contains": "...", "gap_label": "..."}, ...]}
//
// The test extracts the Python parser snippet from the hook script and runs it
// via python3/python with the fixture content on stdin. This validates the
// parser without invoking gg or git, giving a fast, isolated regression gate.
//
// Parser output format (tab-separated per line): num\tac_text\tgap_label
// Each entry assertion checks: num matches, text_contains is a substring of
// ac_text, and gap_label matches exactly. This catches both count drift and
// content/label drift without being brittle to exact wording.
package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// acParserEntry is one row in the expected JSON entries list.
type acParserEntry struct {
	Num          int    `json:"num"`
	TextContains string `json:"text_contains"`
	GapLabel     string `json:"gap_label"`
}

// acParserExpected is the shape of each *.expected.json fixture file.
type acParserExpected struct {
	Count   int             `json:"count"`
	Entries []acParserEntry `json:"entries"`
}

// parserRow is one parsed output row from the Python parser.
type parserRow struct {
	num      int
	acText   string
	gapLabel string
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
// returns the parsed rows. Each output line is tab-separated: num\ttext\tgap_label.
func runPythonParser(t *testing.T, pyCode, detail string) []parserRow {
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

	var rows []parserRow
	for _, line := range strings.Split(raw, "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		num := 0
		if len(parts) >= 1 {
			n, err := strconv.Atoi(strings.TrimSpace(parts[0]))
			if err == nil {
				num = n
			}
		}
		text := ""
		if len(parts) >= 2 {
			text = parts[1]
		}
		gap := ""
		if len(parts) >= 3 {
			gap = parts[2]
		}
		rows = append(rows, parserRow{num: num, acText: text, gapLabel: gap})
	}
	return rows
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
			rows := runPythonParser(t, pyCode, string(detailBytes))

			// Assert count.
			if len(rows) != want.Count {
				t.Errorf("count: got %d, want %d", len(rows), want.Count)
				for i, r := range rows {
					t.Logf("  row[%d]: num=%d text=%q gap=%q", i, r.num, r.acText, r.gapLabel)
				}
			}

			// Assert per-entry fields.
			for i, we := range want.Entries {
				if i >= len(rows) {
					t.Errorf("entry[%d]: missing (parser produced only %d rows)", i, len(rows))
					continue
				}
				got := rows[i]
				if got.num != we.Num {
					t.Errorf("entry[%d] num: got %d, want %d (text=%q)", i, got.num, we.Num, got.acText)
				}
				if we.TextContains != "" && !strings.Contains(got.acText, we.TextContains) {
					t.Errorf("entry[%d] text: %q does not contain %q", i, got.acText, we.TextContains)
				}
				if got.gapLabel != we.GapLabel {
					t.Errorf("entry[%d] gap_label: got %q, want %q", i, got.gapLabel, we.GapLabel)
				}
			}
		})
	}
}
