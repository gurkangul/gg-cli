package adapters_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gurkangul/gg-cli/internal/adapters"
)

func TestManagedBlockInjectFreshFile(t *testing.T) {
	dir := t.TempDir()
	a := &adapters.Adapter{Name: "test", Format: adapters.ManagedBlock}
	path := filepath.Join(dir, "CONVENTIONS.md")
	content := "## gg rules"

	result, err := adapters.Inject(a, path, content)
	if err != nil {
		t.Fatalf("Inject: %v", err)
	}
	if !result.Created {
		t.Error("expected Created=true for fresh file")
	}

	data, _ := os.ReadFile(path)
	body := string(data)
	if !strings.Contains(body, "<!-- gg-managed-start -->") {
		t.Error("managed-start marker missing")
	}
	if !strings.Contains(body, "<!-- gg-managed-end -->") {
		t.Error("managed-end marker missing")
	}
	if !strings.Contains(body, "## gg rules") {
		t.Error("content not injected")
	}
}

func TestManagedBlockInjectPreservesUserContent(t *testing.T) {
	dir := t.TempDir()
	a := &adapters.Adapter{Name: "test", Format: adapters.ManagedBlock}
	path := filepath.Join(dir, "CONVENTIONS.md")

	// Write initial file with user content + managed block.
	initial := "# My Conventions\n\nUser rules here.\n\n<!-- gg-managed-start -->\nold content\n<!-- gg-managed-end -->\n\nMore user rules.\n"
	if err := os.WriteFile(path, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := adapters.Inject(a, path, "new gg content")
	if err != nil {
		t.Fatalf("Inject: %v", err)
	}

	data, _ := os.ReadFile(path)
	body := string(data)
	if !strings.Contains(body, "# My Conventions") {
		t.Error("user header was removed")
	}
	if !strings.Contains(body, "User rules here.") {
		t.Error("user content before block was removed")
	}
	if !strings.Contains(body, "More user rules.") {
		t.Error("user content after block was removed")
	}
	if !strings.Contains(body, "new gg content") {
		t.Error("new content not injected")
	}
	if strings.Contains(body, "old content") {
		t.Error("old gg content not replaced")
	}
}

func TestManagedBlockCheckUpToDate(t *testing.T) {
	dir := t.TempDir()
	a := &adapters.Adapter{Name: "test", Format: adapters.ManagedBlock}
	path := filepath.Join(dir, "rules.md")
	content := "## gg rules"

	if _, err := adapters.Inject(a, path, content); err != nil {
		t.Fatalf("Inject: %v", err)
	}

	upToDate, drift, err := adapters.Check(a, path, content)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !upToDate {
		t.Errorf("expected up-to-date, got drift: %s", drift)
	}
}

func TestManagedBlockCheckDrift(t *testing.T) {
	dir := t.TempDir()
	a := &adapters.Adapter{Name: "test", Format: adapters.ManagedBlock}
	path := filepath.Join(dir, "rules.md")

	if _, err := adapters.Inject(a, path, "old content"); err != nil {
		t.Fatalf("Inject: %v", err)
	}

	upToDate, drift, err := adapters.Check(a, path, "new content")
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if upToDate {
		t.Error("expected drift to be detected")
	}
	if drift == "" {
		t.Error("expected non-empty drift message")
	}
}

func TestFullReplaceInject(t *testing.T) {
	dir := t.TempDir()
	a := &adapters.Adapter{Name: "test", Format: adapters.FullReplace}
	path := filepath.Join(dir, ".cursorrules")

	result, err := adapters.Inject(a, path, "cursor rules")
	if err != nil {
		t.Fatalf("Inject: %v", err)
	}
	if !result.Created {
		t.Error("expected Created=true")
	}

	data, _ := os.ReadFile(path)
	body := string(data)
	if !strings.Contains(body, "cursor rules") {
		t.Error("content not written")
	}
	if !strings.Contains(body, "managed by gg adapt") {
		t.Error("header comment missing")
	}
}

func TestIdempotent(t *testing.T) {
	dir := t.TempDir()
	a := &adapters.Adapter{Name: "test", Format: adapters.ManagedBlock}
	path := filepath.Join(dir, "rules.md")
	content := "## gg rules"

	for i := 0; i < 3; i++ {
		if _, err := adapters.Inject(a, path, content); err != nil {
			t.Fatalf("Inject iteration %d: %v", i, err)
		}
	}

	data, _ := os.ReadFile(path)
	count := strings.Count(string(data), "<!-- gg-managed-start -->")
	if count != 1 {
		t.Errorf("expected exactly 1 managed-start marker, got %d", count)
	}
}
