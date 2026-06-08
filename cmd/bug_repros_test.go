package cmd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunSingleRepro_ScrubsInboxSkipEnv(t *testing.T) {
	t.Setenv("GG_ALLOW_INBOX_SKIP", "should-not-leak")

	tmp := t.TempDir()
	script := filepath.Join(tmp, "repro.sh")
	content := "#!/bin/sh\n" +
		"if [ -n \"${GG_ALLOW_INBOX_SKIP:-}\" ]; then\n" +
		"  echo leaked\n" +
		"  exit 42\n" +
		"fi\n" +
		"echo clean\n" +
		"exit 0\n"
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	res := runSingleRepro(context.Background(), "BUG-TEST", script)
	if !res.passed {
		t.Fatalf("expected pass when env scrubbed, got failed output=%q", res.output)
	}
	if strings.Contains(res.output, "leaked") {
		t.Fatalf("unexpected leaked marker in output: %q", res.output)
	}
}

// BUG-087: a *_test.go repro must run via `go test`, not `sh`. Before the fix
// every repro ran as `sh <path>`, so the 19 bugs that register a *_test.go file
// failed in ~3ms and blocked task closure.
func TestReproCommand_GoTestForTestFiles(t *testing.T) {
	tmp := t.TempDir()
	tf := filepath.Join(tmp, "bug_xyz_test.go")
	src := "package x\n\nimport \"testing\"\n\nfunc TestAlpha(t *testing.T) {}\nfunc TestBeta(t *testing.T) {}\n"
	if err := os.WriteFile(tf, []byte(src), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	c := reproCommand(context.Background(), tf)
	if c.Args[0] != "go" {
		t.Fatalf("expected go command for *_test.go, got %q (%v)", c.Args[0], c.Args)
	}
	args := strings.Join(c.Args, " ")
	if !strings.Contains(args, "test") || !strings.Contains(args, "-run") {
		t.Fatalf("expected `go test -run`, got %v", c.Args)
	}
	if !strings.Contains(args, "TestAlpha") || !strings.Contains(args, "TestBeta") {
		t.Fatalf("expected both test funcs in -run filter, got %v", c.Args)
	}
}

func TestReproCommand_ShellForScripts(t *testing.T) {
	c := reproCommand(context.Background(), "/tmp/repro.sh")
	if c.Args[0] != "sh" {
		t.Fatalf("expected sh command for .sh repro, got %q (%v)", c.Args[0], c.Args)
	}
}

func TestTestFuncNames_TopLevelTestsOnly(t *testing.T) {
	tmp := t.TempDir()
	tf := filepath.Join(tmp, "x_test.go")
	src := "package x\nfunc TestOne(t *testing.T){}\nfunc helper(){}\n\tfunc TestIndented(t *testing.T){}\nfunc TestTwo(t *testing.T){}\n"
	if err := os.WriteFile(tf, []byte(src), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	got := testFuncNames(tf)
	if len(got) != 2 || got[0] != "TestOne" || got[1] != "TestTwo" {
		t.Fatalf("expected [TestOne TestTwo] (top-level only), got %v", got)
	}
}
