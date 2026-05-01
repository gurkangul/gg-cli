#!/bin/sh
set -eu

test_file="internal/templates/bug039_repro_test.go"
cleanup() {
  rm -f "$test_file"
}
trap cleanup EXIT INT TERM

cat > "$test_file" <<'GO'
package templates

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBUG039_PreTaskDoneGoScrubsACBypass(t *testing.T) {
	root := t.TempDir()
	hookDir := filepath.Join(root, ".gg", "hooks", "pre-task-done.d")
	if err := os.MkdirAll(hookDir, 0o755); err != nil {
		t.Fatal(err)
	}
	hookPath := filepath.Join(hookDir, "10-go-verify.sh")
	body := strings.ReplaceAll(PreTaskDoneGoHook, "__GG_SUBDIR__", ".")
	if err := os.WriteFile(hookPath, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}

	binDir := filepath.Join(root, "fakebin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(root, "go.log")
	fakeGo := `#!/bin/sh
val="${GG_ALLOW_INCOMPLETE_AC:-<unset>}"
printf '%s|%s\n' "$1" "$val" >> "` + logPath + `"
exit 0
`
	if err := os.WriteFile(filepath.Join(binDir, "go"), []byte(fakeGo), 0o755); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("/bin/sh", hookPath)
	cmd.Dir = root
	cmd.Env = append(os.Environ(),
		"PATH="+binDir+":"+os.Getenv("PATH"),
		"GG_TASK_ID=TASK-370",
		"GG_ALLOW_INCOMPLETE_AC=leak-probe",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("hook failed: %v\n%s", err, out)
	}
	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	log := string(logBytes)
	if strings.Contains(log, "test|leak-probe") {
		t.Fatalf("GG_ALLOW_INCOMPLETE_AC leaked into go test:\n%s", log)
	}
	if !strings.Contains(log, "test|<unset>") {
		t.Fatalf("expected scrubbed go test invocation, got:\n%s", log)
	}
}
GO

go test ./internal/templates -run TestBUG039_PreTaskDoneGoScrubsACBypass -count=1
