package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gurkangul/gg-cli/internal/templates"
)

func TestHookTemplateMarkerWrittenOnFreshInstall(t *testing.T) {
	f := newHookInstallFixture(t, true, false)
	if err := runDoctorInstallTaskHooks(); err != nil {
		t.Fatalf("installer: %v", err)
	}

	path := filepath.Join(f.preDir, "10-go-verify.sh")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read hook: %v", err)
	}
	name, sha, ok := templates.ParseHookTemplateMarker(string(data))
	if !ok {
		t.Fatalf("expected hook marker in:\n%s", string(data))
	}
	if name != "PreTaskDoneGoHook" {
		t.Fatalf("marker name = %q, want PreTaskDoneGoHook", name)
	}
	if sha != templates.HookTemplateHash(string(data)) {
		t.Fatalf("marker sha = %s, actual body sha = %s", sha, templates.HookTemplateHash(string(data)))
	}
}

func TestHookTemplateDriftDetectionSeesEditedBody(t *testing.T) {
	f := newHookInstallFixture(t, true, false)
	if err := runDoctorInstallTaskHooks(); err != nil {
		t.Fatalf("installer: %v", err)
	}
	path := filepath.Join(f.preDir, "10-go-verify.sh")
	appendHookDrift(t, path)

	check := findHookTemplateCheck(t, path)
	if check.state != hookTemplateDrift {
		t.Fatalf("state = %s, want drift", check.state)
	}
}

func TestRefreshHooksOverwritesDriftAndCreatesBackup(t *testing.T) {
	f := newHookInstallFixture(t, true, false)
	if err := runDoctorInstallTaskHooks(); err != nil {
		t.Fatalf("installer: %v", err)
	}
	path := filepath.Join(f.preDir, "10-go-verify.sh")
	appendHookDrift(t, path)

	if err := runDoctorRefreshHooks(false); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if matches, err := filepath.Glob(path + ".bak.*"); err != nil || len(matches) != 1 {
		t.Fatalf("expected one backup, matches=%v err=%v", matches, err)
	}
	check := findHookTemplateCheck(t, path)
	if check.state != hookTemplateInSync {
		t.Fatalf("state = %s, want in-sync", check.state)
	}
}

func TestRefreshHooksPreservesUserCustomizedWithoutMarker(t *testing.T) {
	f := newHookInstallFixture(t, true, false)
	if err := os.MkdirAll(f.preDir, 0o755); err != nil {
		t.Fatalf("mkdir pre hooks: %v", err)
	}
	path := filepath.Join(f.preDir, "10-go-verify.sh")
	custom := "#!/bin/sh\n# custom hook without marker\nexit 0\n"
	if err := os.WriteFile(path, []byte(custom), 0o755); err != nil {
		t.Fatalf("write custom hook: %v", err)
	}

	if err := runDoctorRefreshHooks(false); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read hook: %v", err)
	}
	if string(got) != custom {
		t.Fatalf("custom hook was overwritten:\n%s", string(got))
	}
}

func TestRefreshHooksForceOverwritesUserCustomizedWithBackup(t *testing.T) {
	f := newHookInstallFixture(t, true, false)
	if err := os.MkdirAll(f.preDir, 0o755); err != nil {
		t.Fatalf("mkdir pre hooks: %v", err)
	}
	path := filepath.Join(f.preDir, "10-go-verify.sh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n# custom\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write custom hook: %v", err)
	}

	if err := runDoctorRefreshHooks(true); err != nil {
		t.Fatalf("force refresh: %v", err)
	}
	if matches, err := filepath.Glob(path + ".bak.*"); err != nil || len(matches) != 1 {
		t.Fatalf("expected one backup, matches=%v err=%v", matches, err)
	}
	check := findHookTemplateCheck(t, path)
	if check.state != hookTemplateInSync {
		t.Fatalf("state = %s, want in-sync", check.state)
	}
}

func TestRefreshHooksIdempotentWhenInSync(t *testing.T) {
	f := newHookInstallFixture(t, true, false)
	if err := runDoctorInstallTaskHooks(); err != nil {
		t.Fatalf("installer: %v", err)
	}
	out := captureHookRefreshStdout(t, func() {
		if err := runDoctorRefreshHooks(false); err != nil {
			t.Fatalf("refresh: %v", err)
		}
	})
	if !strings.Contains(out, "all hook templates in sync") {
		t.Fatalf("expected in-sync message, got:\n%s", out)
	}
	if matches, err := filepath.Glob(filepath.Join(f.preDir, "*.bak.*")); err != nil || len(matches) != 0 {
		t.Fatalf("expected no backups, matches=%v err=%v", matches, err)
	}
}

func appendHookDrift(t *testing.T, path string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o755) //nolint:gosec
	if err != nil {
		t.Fatalf("open hook: %v", err)
	}
	defer func() { _ = f.Close() }()
	if _, err := f.WriteString("\n# local drift\n"); err != nil {
		t.Fatalf("append drift: %v", err)
	}
}

func findHookTemplateCheck(t *testing.T, path string) hookTemplateCheck {
	t.Helper()
	checks, err := checkHookTemplates()
	if err != nil {
		t.Fatalf("check hook templates: %v", err)
	}
	for _, check := range checks {
		if check.spec.path == path {
			return check
		}
	}
	t.Fatalf("no check for %s", path)
	return hookTemplateCheck{}
}

func captureHookRefreshStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	fn()
	_ = w.Close()
	os.Stdout = orig
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	return buf.String()
}
