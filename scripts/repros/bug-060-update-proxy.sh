#!/bin/sh
set -eu

cat > cmd/update_bug060_repro_test.go <<'GO'
package cmd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBUG060UpdatePinsDirectLatest(t *testing.T) {
	oldVersion := rootCmd.Version
	oldJSON := jsonOutput
	oldSkipSync := updateSkipSync
	oldForce := updateForce
	rootCmd.Version = "v0.3.9"
	jsonOutput = false
	updateSkipSync = true
	updateForce = false
	t.Cleanup(func() {
		rootCmd.Version = oldVersion
		jsonOutput = oldJSON
		updateSkipSync = oldSkipSync
		updateForce = oldForce
	})

	logPath := filepath.Join(t.TempDir(), "go.log")
	installBUG060FakeGo(t, "v0.3.9", "v0.3.10", logPath)

	stdout, stderr, err := captureUpdateOutput(t, func() error {
		cmd := *updateCmd
		cmd.SetContext(context.Background())
		return runUpdate(&cmd, nil)
	})
	if err != nil {
		t.Fatalf("update failed: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}
	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read go log: %v", err)
	}
	got := string(logBytes)
	want := "install github.com/gurkangul/gg-cli/cmd/gg@v0.3.10 GOPROXY=direct"
	if !strings.Contains(got, want) {
		t.Fatalf("go install log = %q, want %q", got, want)
	}
}

func installBUG060FakeGo(t *testing.T, proxyVersion, directVersion, logPath string) {
	t.Helper()
	dir := t.TempDir()
	goPathRoot := filepath.Join(t.TempDir(), "gopath")
	goPath := filepath.Join(dir, "go")
	t.Setenv("GG_UPDATE_TEST_LOG", logPath)
	script := "#!/bin/sh\n" +
		"if [ \"$1 $2 $3\" = \"list -m -json\" ]; then\n" +
		"  if [ \"$GOPROXY\" = \"direct\" ]; then\n" +
		"    printf '{\"Path\":\"github.com/gurkangul/gg-cli\",\"Version\":\"" + directVersion + "\"}\\n'\n" +
		"  else\n" +
		"    printf '{\"Path\":\"github.com/gurkangul/gg-cli\",\"Version\":\"" + proxyVersion + "\"}\\n'\n" +
		"  fi\n" +
		"  exit 0\n" +
		"fi\n" +
		"if [ \"$1\" = \"install\" ]; then\n" +
		"  printf 'install %s GOPROXY=%s\\n' \"$2\" \"${GOPROXY:-}\" >> \"$GG_UPDATE_TEST_LOG\"\n" +
		"  exit 0\n" +
		"fi\n" +
		"if [ \"$1 $2\" = \"env GOBIN\" ]; then\n" +
		"  printf '\\n'\n" +
		"  exit 0\n" +
		"fi\n" +
		"if [ \"$1 $2\" = \"env GOPATH\" ]; then\n" +
		"  printf '" + goPathRoot + "\\n'\n" +
		"  exit 0\n" +
		"fi\n" +
		"echo unexpected go args: \"$@\" >&2\n" +
		"exit 1\n"
	if err := os.WriteFile(goPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake go: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}
GO

trap 'rm -f cmd/update_bug060_repro_test.go' EXIT

go test ./cmd -run TestBUG060UpdatePinsDirectLatest -count=1
