#!/bin/sh
set -eu

repo_root=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
test_file="$repo_root/internal/agenthooks/bug023_repro_test.go"

cleanup() {
  rm -f "$test_file"
}
trap cleanup EXIT INT TERM

cat > "$test_file" <<'EOF'
package agenthooks

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBUG023LegacyManagedBlocksAreDrifted(t *testing.T) {
	t.Run("master role v1 and v2 coexist", func(t *testing.T) {
		dir := t.TempDir()
		content := "# project\n\n" +
			"<!-- gg:master-role:begin v1 -->\nold v1\n\n" +
			"<!-- gg:master-role:begin v2 -->\nold v2\n" +
			"<!-- gg:master-role:end -->\n"
		if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := CheckMasterRole(dir).Status; got != MasterRoleDRIFTED {
			t.Fatalf("CheckMasterRole status = %s, want DRIFTED", got)
		}
	})

	t.Run("contract prior version", func(t *testing.T) {
		dir := t.TempDir()
		content := "<!-- gg:contract:begin v0 -->\nold contract\n<!-- gg:contract:end -->\n"
		if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		results := CheckContract(dir)
		var found *ContractCheckResult
		for i, r := range results {
			if r.AgentName == "claude" {
				found = &results[i]
				break
			}
		}
		if found == nil {
			t.Fatal("missing claude contract check result")
		}
		if found.Status != ContractDRIFTED {
			t.Fatalf("CheckContract status = %s, want DRIFTED", found.Status)
		}
	})
}
EOF

go test ./internal/agenthooks -run TestBUG023LegacyManagedBlocksAreDrifted -count=1
