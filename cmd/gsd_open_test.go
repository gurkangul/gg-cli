package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGSDOpen_UsesFakeTerminalWithoutHeadless(t *testing.T) {
	setupGGDir(t)
	t.Setenv("GG_TERMINAL", "fake")
	t.Setenv("GG_AGENT", "codex")
	t.Setenv("GG_ROLE", "developer")
	t.Setenv("PATH", fakeExecutableOnPath(t, "gsd"))

	_, _, err := execCmd(t, "gsd", "open")
	if err != nil {
		t.Fatalf("gg gsd open: %v", err)
	}
}

func TestGSDOpen_MissingGSDIsClear(t *testing.T) {
	setupGGDir(t)
	t.Setenv("GG_TERMINAL", "fake")
	t.Setenv("PATH", t.TempDir())

	_, _, err := execCmd(t, "gsd", "open")
	if err == nil {
		t.Fatal("expected missing GSD error")
	}
	if !strings.Contains(err.Error(), `GSD command "gsd" not found on PATH`) {
		t.Fatalf("error = %v, want missing GSD guidance", err)
	}
}

func TestBuildGSDOpenLaunchCommand(t *testing.T) {
	t.Setenv("GG_AGENT", "codex")
	t.Setenv("GG_ROLE", "developer")

	got := buildGSDOpenLaunchCommand("/tmp/project with spaces", "gsd")
	for _, want := range []string{
		"cd '/tmp/project with spaces'",
		"export GG_AGENT='codex'",
		"export GG_ROLE='developer'",
		"exec gsd",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("launch command missing %q\ngot: %s", want, got)
		}
	}
	if strings.Contains(got, "headless") {
		t.Fatalf("launch command must start interactive GSD, got: %s", got)
	}
}

func fakeExecutableOnPath(t *testing.T, name string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake %s: %v", name, err)
	}
	return dir
}
