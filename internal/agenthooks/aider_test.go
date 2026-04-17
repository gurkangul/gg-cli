package agenthooks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestAider_Detect(t *testing.T) {
	root := t.TempDir()
	if (&aiderInstaller{}).Detect(root) {
		t.Error("expected Detect false on empty dir")
	}
	if err := os.WriteFile(filepath.Join(root, ".aider.conf.yml"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !(&aiderInstaller{}).Detect(root) {
		t.Error("expected Detect true with .aider.conf.yml")
	}
}

func TestAider_Install_CreateAddsReadKey(t *testing.T) {
	root := t.TempDir()
	res, err := (&aiderInstaller{}).Install(root, Options{})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if res.Action != ActionCreated {
		t.Errorf("Action = %q, want %q", res.Action, ActionCreated)
	}
	raw, err := os.ReadFile(filepath.Join(root, ".aider.conf.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := yaml.Unmarshal(raw, &m); err != nil {
		t.Fatalf("not valid YAML: %v\n%s", err, raw)
	}
	reads := aiderNormalizeRead(m["read"])
	if !containsString(reads, "AGENTS.md") {
		t.Errorf("read key missing AGENTS.md: %v", reads)
	}
}

func TestAider_Install_IdempotentWhenAlreadyPresent(t *testing.T) {
	root := t.TempDir()
	conf := "read:\n  - AGENTS.md\n  - README.md\n"
	if err := os.WriteFile(filepath.Join(root, ".aider.conf.yml"), []byte(conf), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := (&aiderInstaller{}).Install(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Action != ActionUpToDate {
		t.Errorf("Action = %q, want %q", res.Action, ActionUpToDate)
	}
}

func TestAider_Install_MergePreservesExisting(t *testing.T) {
	root := t.TempDir()
	conf := "read:\n  - README.md\nother: value\n"
	if err := os.WriteFile(filepath.Join(root, ".aider.conf.yml"), []byte(conf), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := (&aiderInstaller{}).Install(root, Options{}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(root, ".aider.conf.yml"))
	var m map[string]any
	if err := yaml.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	reads := aiderNormalizeRead(m["read"])
	if !containsString(reads, "README.md") {
		t.Errorf("lost existing README.md: %v", reads)
	}
	if !containsString(reads, "AGENTS.md") {
		t.Errorf("missing AGENTS.md: %v", reads)
	}
	if m["other"] != "value" {
		t.Errorf("lost unrelated key 'other': %v", m["other"])
	}
}

func TestAider_Install_HandlesScalarReadValue(t *testing.T) {
	root := t.TempDir()
	// Some users write `read: README.md` as a scalar; we must normalize.
	conf := "read: README.md\n"
	if err := os.WriteFile(filepath.Join(root, ".aider.conf.yml"), []byte(conf), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := (&aiderInstaller{}).Install(root, Options{}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(root, ".aider.conf.yml"))
	if !strings.Contains(string(raw), "AGENTS.md") {
		t.Errorf("AGENTS.md not added: %s", raw)
	}
	if !strings.Contains(string(raw), "README.md") {
		t.Errorf("lost scalar README.md during normalization: %s", raw)
	}
}

func TestAider_Normalize_EmptyCases(t *testing.T) {
	if got := aiderNormalizeRead(nil); got != nil {
		t.Errorf("nil → %v, want nil", got)
	}
	if got := aiderNormalizeRead(""); got != nil {
		t.Errorf("\"\" → %v, want nil", got)
	}
	if got := aiderNormalizeRead([]any{}); len(got) != 0 {
		t.Errorf("[] → %v, want empty", got)
	}
}
