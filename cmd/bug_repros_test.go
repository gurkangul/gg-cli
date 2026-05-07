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
