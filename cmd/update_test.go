package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompareSemver(t *testing.T) {
	tests := []struct {
		name string
		a    string
		b    string
		want int
		ok   bool
	}{
		{name: "older", a: "v0.1.0", b: "v0.2.0", want: -1, ok: true},
		{name: "newer", a: "v1.2.1", b: "v1.2.0", want: 1, ok: true},
		{name: "same", a: "v1.2.3", b: "v1.2.3", want: 0, ok: true},
		{name: "release beats prerelease", a: "v1.0.0", b: "v1.0.0-alpha", want: 1, ok: true},
		{name: "dev not comparable", a: "dev-abcdef0", b: "v1.0.0", ok: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := compareSemver(tt.a, tt.b)
			if ok != tt.ok {
				t.Fatalf("ok = %v, want %v", ok, tt.ok)
			}
			if ok && got != tt.want {
				t.Fatalf("compareSemver(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestEnvTruthy(t *testing.T) {
	for _, v := range []string{"1", "true", "TRUE", "yes", "on", "y"} {
		if !envTruthy(v) {
			t.Fatalf("envTruthy(%q) = false, want true", v)
		}
	}
	for _, v := range []string{"", "0", "false", "off", "no"} {
		if envTruthy(v) {
			t.Fatalf("envTruthy(%q) = true, want false", v)
		}
	}
}

func TestShouldInstallUpdate(t *testing.T) {
	tests := []struct {
		name  string
		info  updateInfo
		force bool
		want  bool
	}{
		{name: "up to date", info: updateInfo{Comparable: true, Update: false}, want: false},
		{name: "update available", info: updateInfo{Comparable: true, Update: true}, want: true},
		{name: "dev build installs", info: updateInfo{Comparable: false, Update: false}, want: true},
		{name: "force installs", info: updateInfo{Comparable: true, Update: false}, force: true, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := shouldInstallUpdate(tt.info, tt.force)
			if got != tt.want {
				t.Fatalf("shouldInstallUpdate() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEmitUpdateNotice_OptInOnly(t *testing.T) {
	oldVersion := rootCmd.Version
	rootCmd.Version = "v0.1.0"
	t.Cleanup(func() { rootCmd.Version = oldVersion })
	installFakeGo(t, "v0.2.0")

	t.Setenv(updateCheckEnvVar, "")
	var buf bytes.Buffer
	emitUpdateNotice(&buf)
	if buf.Len() != 0 {
		t.Fatalf("notice without opt-in:\n%s", buf.String())
	}

	t.Setenv(updateCheckEnvVar, "1")
	emitUpdateNotice(&buf)
	got := buf.String()
	if !strings.Contains(got, "GG UPDATE AVAILABLE: v0.1.0") || !strings.Contains(got, "run: gg update") {
		t.Fatalf("missing update notice:\n%s", got)
	}
}

func TestUpdateJSON_StdoutIsSingleObject(t *testing.T) {
	oldVersion := rootCmd.Version
	rootCmd.Version = "v0.1.0"
	t.Cleanup(func() { rootCmd.Version = oldVersion })
	installFakeGo(t, "v0.2.0")

	oldJSON := jsonOutput
	oldSkipSync := updateSkipSync
	jsonOutput = true
	updateSkipSync = true
	t.Cleanup(func() {
		jsonOutput = oldJSON
		updateSkipSync = oldSkipSync
	})

	stdout, stderr, err := captureUpdateOutput(t, func() error {
		cmd := *updateCmd
		cmd.SetContext(context.Background())
		return runUpdate(&cmd, nil)
	})
	if err != nil {
		t.Fatalf("update --json failed: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}
	if strings.Contains(stdout, "install stdout") {
		t.Fatalf("child stdout leaked into JSON stdout:\n%s", stdout)
	}
	var got updateResult
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not one JSON object: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}
	if !got.Installed || got.Synced {
		t.Fatalf("unexpected result: %+v", got)
	}
	if !strings.Contains(stderr, "install stdout") {
		t.Fatalf("expected child install output on stderr, got:\n%s", stderr)
	}
}

func captureUpdateOutput(t *testing.T, fn func() error) (string, string, error) {
	t.Helper()
	oldStdout := os.Stdout
	oldStderr := os.Stderr
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = stdoutW
	os.Stderr = stderrW
	runErr := fn()
	_ = stdoutW.Close()
	_ = stderrW.Close()
	os.Stdout = oldStdout
	os.Stderr = oldStderr
	outBytes, _ := io.ReadAll(stdoutR)
	errBytes, _ := io.ReadAll(stderrR)
	_ = stdoutR.Close()
	_ = stderrR.Close()
	return string(outBytes), string(errBytes), runErr
}

func installFakeGo(t *testing.T, version string) {
	t.Helper()
	dir := t.TempDir()
	goPathRoot := filepath.Join(t.TempDir(), "gopath")
	goPath := filepath.Join(dir, "go")
	script := "#!/bin/sh\n" +
		"if [ \"$1 $2 $3\" = \"list -m -json\" ]; then\n" +
		"  printf '{\"Path\":\"github.com/gurkangul/gg-cli\",\"Version\":\"" + version + "\"}\\n'\n" +
		"  exit 0\n" +
		"fi\n" +
		"if [ \"$1\" = \"install\" ]; then\n" +
		"  echo install stdout\n" +
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
