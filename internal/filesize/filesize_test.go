package filesize

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestParseLimit(t *testing.T) {
	cases := []struct {
		path  string
		limit int
	}{
		{"foo_test.go", DefaultTestLimit},
		{"bar.go", DefaultSourceLimit},
		{"component.test.ts", DefaultTestLimit},
		{"service.spec.js", DefaultTestLimit},
		{"__tests__/util.spec.py", DefaultTestLimit},
		{"handler.tsx", DefaultSourceLimit},
		{"model.rs", DefaultSourceLimit},
		{"Main.java", DefaultSourceLimit},
	}
	for _, c := range cases {
		got := ParseLimit(c.path)
		if got != c.limit {
			t.Errorf("ParseLimit(%q) = %d; want %d", c.path, got, c.limit)
		}
	}
}

func TestShouldExclude(t *testing.T) {
	excluded := []string{
		"vendor/foo.go",
		"node_modules/lib.js",
		"testdata/fixture.go",
		"_bmad/agent.md",
		"_bmad-output/x.go",
		"docs/cli/task.md",
		"proto/gen.pb.go",
		"model_generated.go",
		"README.md",
		"Makefile",
	}
	for _, p := range excluded {
		if !ShouldExclude(p) {
			t.Errorf("ShouldExclude(%q) = false; want true", p)
		}
	}

	included := []string{
		"cmd/task.go",
		"internal/store/crud.go",
		"src/index.ts",
		"lib/util.js",
		"app/main.py",
	}
	for _, p := range included {
		if ShouldExclude(p) {
			t.Errorf("ShouldExclude(%q) = true; want false", p)
		}
	}
}

func TestBaselineRoundtrip(t *testing.T) {
	dir := t.TempDir()
	ggDir := filepath.Join(dir, ".gg")
	if err := os.MkdirAll(ggDir, 0o755); err != nil {
		t.Fatal(err)
	}

	b := &Baseline{
		Files: map[string]int{
			"cmd/big.go":  1200,
			"cmd/huge.go": 900,
		},
	}
	if err := WriteBaseline(dir, b); err != nil {
		t.Fatalf("WriteBaseline: %v", err)
	}

	got, err := ReadBaseline(dir)
	if err != nil {
		t.Fatalf("ReadBaseline: %v", err)
	}
	if got.Version != 1 {
		t.Errorf("version = %d; want 1", got.Version)
	}
	if got.Files["cmd/big.go"] != 1200 {
		t.Errorf("cmd/big.go = %d; want 1200", got.Files["cmd/big.go"])
	}

	// Verify the JSON is stable (deterministic enough to round-trip).
	data, _ := os.ReadFile(filepath.Join(ggDir, "file-size-baseline.json"))
	var reparsed Baseline
	if err := json.Unmarshal(data, &reparsed); err != nil {
		t.Fatalf("re-parse: %v", err)
	}
	if reparsed.Files["cmd/huge.go"] != 900 {
		t.Errorf("re-parsed cmd/huge.go = %d; want 900", reparsed.Files["cmd/huge.go"])
	}
}

func TestReadBaselineMissing(t *testing.T) {
	dir := t.TempDir()
	b, err := ReadBaseline(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if b == nil || b.Files == nil {
		t.Fatal("expected non-nil baseline with empty Files map")
	}
}

func TestCheckViolations(t *testing.T) {
	b := &Baseline{
		Files: map[string]int{
			"cmd/big.go": 600,
		},
	}

	files := map[string]int{
		"cmd/small.go": 200,        // under limit — no violation
		"cmd/over.go":  510,        // new file > 500 — violation
		"cmd/big.go":   650,        // in baseline (600), grew > limit → violation
		"cmd/shrink.go": 490,       // would be grandfathered if baseline said 600
		"a_test.go":    820,        // test file > 800 — violation
		"b_test.go":    750,        // test file ≤ 800 — OK
	}

	// Add shrink to baseline so it is grandfathered.
	b.Files["cmd/shrink.go"] = 600

	violations := CheckViolations(files, b, true)

	violated := map[string]bool{}
	for _, v := range violations {
		violated[v.Path] = true
	}

	if violated["cmd/small.go"] {
		t.Error("cmd/small.go should not be a violation")
	}
	if !violated["cmd/over.go"] {
		t.Error("cmd/over.go should be a violation (new file > 500)")
	}
	if !violated["cmd/big.go"] {
		t.Error("cmd/big.go should be a violation (grew beyond baseline and limit)")
	}
	if violated["cmd/shrink.go"] {
		t.Error("cmd/shrink.go should not be a violation (shrunk below baseline)")
	}
	if !violated["a_test.go"] {
		t.Error("a_test.go should be a violation (820 > 800)")
	}
	if violated["b_test.go"] {
		t.Error("b_test.go should not be a violation (750 ≤ 800)")
	}
}

func TestCheckViolationsNoBaseline(t *testing.T) {
	files := map[string]int{
		"big.go":   510,
		"small.go": 100,
	}
	violations := CheckViolations(files, nil, false)
	if len(violations) != 1 || violations[0].Path != "big.go" {
		t.Errorf("expected one violation for big.go, got %v", violations)
	}
}
