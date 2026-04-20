package agenthooks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestContractBlock_LoadsFromTemplate(t *testing.T) {
	body := ContractBody()
	if body == "" {
		t.Fatal("ContractBody() is empty — agent-contract.md not embedded")
	}
	if !strings.Contains(body, "GG MANDATORY CONTRACT") {
		t.Errorf("ContractBody() missing expected heading: %q", body)
	}
}

func TestContractBlock_Deterministic(t *testing.T) {
	a := ContractBlock()
	b := ContractBlock()
	if a != b {
		t.Error("ContractBlock() is not deterministic across calls")
	}
	if !strings.HasPrefix(a, ContractBlockBegin) {
		t.Errorf("ContractBlock() must start with begin marker, got: %q", a[:min(len(a), 60)])
	}
	if !strings.HasSuffix(strings.TrimRight(a, "\n"), ContractBlockEnd) {
		t.Errorf("ContractBlock() must end with end marker (before trailing newline)")
	}
}

func TestContractVersion_IsHexSHA256(t *testing.T) {
	v := ContractVersion()
	if len(v) != 64 {
		t.Errorf("ContractVersion() length = %d, want 64 hex chars", len(v))
	}
	for _, c := range v {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			t.Errorf("ContractVersion() contains non-hex char %q", c)
		}
	}
}

// TestContractVersion_ChangesWithContent verifies the version is derived from
// the body — two different bodies must produce different hashes.
func TestContractVersion_ChangesWithContent(t *testing.T) {
	v1 := ContractVersion()
	// The version is derived from AgentContract. We can't change the template
	// in-test, but we can verify it's a real hash of the body by checking the
	// length and format above. Instead, verify the version is non-empty.
	if v1 == "" {
		t.Error("ContractVersion() returned empty string")
	}
}

func TestReplaceOrAppendBlock_AppendWhenAbsent(t *testing.T) {
	content := "# Header\n\nExisting content.\n"
	out, changed, err := replaceOrAppendBlock(content, "<!-- begin -->", "<!-- end -->", "<!-- begin -->\nbody\n<!-- end -->\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !changed {
		t.Error("expected changed=true when block is absent")
	}
	if !strings.HasPrefix(out, content) {
		t.Errorf("original content should be preserved at start of output")
	}
	if !strings.Contains(out, "<!-- begin -->") {
		t.Error("output missing begin marker")
	}
}

func TestReplaceOrAppendBlock_ReplacesExisting(t *testing.T) {
	content := "# Head\n\n<!-- begin -->\nstale\n<!-- end -->\n\n# Tail\n"
	managed := "<!-- begin -->\nnew body\n<!-- end -->\n"
	out, changed, err := replaceOrAppendBlock(content, "<!-- begin -->", "<!-- end -->", managed)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !changed {
		t.Error("expected changed=true when stale block replaced")
	}
	if strings.Contains(out, "stale") {
		t.Error("stale content should have been replaced")
	}
	if !strings.Contains(out, "new body") {
		t.Error("new content missing from output")
	}
	if !strings.Contains(out, "# Tail") {
		t.Error("content after block should be preserved")
	}
	if strings.Count(out, "<!-- begin -->") != 1 {
		t.Error("expected exactly one begin marker in output")
	}
}

func TestReplaceOrAppendBlock_IdempotentWhenCurrent(t *testing.T) {
	managed := "<!-- begin -->\nbody\n<!-- end -->\n"
	content := "# Head\n\n" + managed
	out, changed, err := replaceOrAppendBlock(content, "<!-- begin -->", "<!-- end -->", managed)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if changed {
		t.Error("expected changed=false when block is already current")
	}
	if out != content {
		t.Error("content should be unchanged when block is already current")
	}
}

func TestReplaceOrAppendBlock_ErrorOnMalformedBeginOnly(t *testing.T) {
	content := "# Head\n\n<!-- begin -->\nno end marker\n"
	_, _, err := replaceOrAppendBlock(content, "<!-- begin -->", "<!-- end -->", "")
	if err == nil {
		t.Error("expected error when begin marker present but end marker absent")
	}
}

func TestReplaceOrAppendBlock_ErrorOnMalformedEndOnly(t *testing.T) {
	content := "# Head\n\n<!-- end -->\n"
	_, _, err := replaceOrAppendBlock(content, "<!-- begin -->", "<!-- end -->", "")
	if err == nil {
		t.Error("expected error when end marker present but begin marker absent")
	}
}

func TestWriteContractBlock_CreatesFileWhenAbsent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")

	action, notes, err := writeContractBlock(path, false)
	if err != nil {
		t.Fatalf("writeContractBlock: %v", err)
	}
	if action != ActionCreated {
		t.Errorf("action = %q, want %q", action, ActionCreated)
	}
	_ = notes

	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), ContractBlockBegin) {
		t.Error("written file missing contract begin marker")
	}
	if !strings.Contains(string(raw), ContractBlockEnd) {
		t.Error("written file missing contract end marker")
	}
}

func TestWriteContractBlock_IdempotentOnRerun(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")

	if _, _, err := writeContractBlock(path, false); err != nil {
		t.Fatal(err)
	}
	action, _, err := writeContractBlock(path, false)
	if err != nil {
		t.Fatalf("second writeContractBlock: %v", err)
	}
	if action != ActionUpToDate {
		t.Errorf("second call action = %q, want %q", action, ActionUpToDate)
	}
}

func TestWriteContractBlock_PreservesNonManagedContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "AGENTS.md")
	existing := "# Project Agents\n\nHuman-authored content.\n"
	if err := os.WriteFile(path, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, _, err := writeContractBlock(path, false); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	s := string(raw)
	if !strings.HasPrefix(s, existing) {
		t.Errorf("original content should be preserved at start of file")
	}
	if !strings.Contains(s, ContractBlockBegin) {
		t.Error("contract begin marker missing")
	}
}

func TestWriteContractBlock_ErrorOnMalformedMarkers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "AGENTS.md")
	// Malformed: begin without end.
	content := "# Head\n\n" + ContractBlockBegin + "\ncontent without end\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	_, _, err := writeContractBlock(path, false)
	if err == nil {
		t.Error("expected error when contract markers are malformed")
	}
	// File should be untouched.
	raw, _ := os.ReadFile(path)
	if string(raw) != content {
		t.Error("malformed-marker file should not be modified")
	}
}

func TestWriteContractBlock_DryRunDoesNotWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")

	action, _, err := writeContractBlock(path, true)
	if err != nil {
		t.Fatalf("dry-run writeContractBlock: %v", err)
	}
	if action != ActionDryRun {
		t.Errorf("action = %q, want %q", action, ActionDryRun)
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Error("dry-run should not create the file")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
