package cmd

import (
	"bytes"
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

func installFakeGo(t *testing.T, version string) {
	t.Helper()
	dir := t.TempDir()
	goPath := filepath.Join(dir, "go")
	script := "#!/bin/sh\n" +
		"if [ \"$1 $2 $3\" = \"list -m -json\" ]; then\n" +
		"  printf '{\"Path\":\"github.com/gurkangul/gg-cli\",\"Version\":\"" + version + "\"}\\n'\n" +
		"  exit 0\n" +
		"fi\n" +
		"echo unexpected go args: \"$@\" >&2\n" +
		"exit 1\n"
	if err := os.WriteFile(goPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake go: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}
