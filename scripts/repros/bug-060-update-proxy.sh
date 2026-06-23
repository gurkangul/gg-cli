#!/bin/sh
set -eu

# BUG-060 (regression guard, rewritten for self-update):
# Originally: `gg update` trusted a stale Go module proxy and installed an older
# @latest than `check` reported. That whole problem class is gone — self-update
# now resolves the latest release directly from the GitHub release API and
# atomically swaps the running binary, so there is no proxy cache to lag behind.
#
# This repro pins the NEW invariant: `gg update` installs the exact latest tag
# advertised by the release API (never an older one), verified against an
# httptest-stubbed API + asset (no real network). It reuses the test helpers in
# cmd/update_test.go (makeReleaseArchive / releaseServer / withStubbedExe /
# captureUpdateOutput), which share the cmd package.

cat > cmd/update_bug060_repro_test.go <<'GO'
package cmd

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"
)

// TestBUG060UpdateInstallsAdvertisedLatest asserts gg update reaches the exact
// latest release tag served by the API and replaces the binary with that
// release's payload — the stale-source regression BUG-060 guarded, now under
// the GitHub-release self-update mechanism.
func TestBUG060UpdateInstallsAdvertisedLatest(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("self-update binary replacement not supported on windows")
	}
	oldVer := version
	oldJSON := jsonOutput
	oldForce := updateForce
	oldFromSource := updateFromSource
	oldSkip := updateSkipSync
	oldLook := updateLookPath
	version = "v0.3.15" // behind latest
	jsonOutput = false
	updateForce = false
	updateFromSource = false
	updateSkipSync = true // avoid invoking the freshly-written fake binary
	updateLookPath = func(string) (string, error) { return "", fmt.Errorf("not found") }
	t.Cleanup(func() {
		version = oldVer
		jsonOutput = oldJSON
		updateForce = oldForce
		updateFromSource = oldFromSource
		updateSkipSync = oldSkip
		updateLookPath = oldLook
	})

	payload := []byte("RELEASE-0.3.16-BINARY")
	archive := makeReleaseArchive(t, payload)
	srv, _ := releaseServer(t, "v0.3.16", archive, false)
	oldBase := ggReleaseAPIBase
	t.Cleanup(func() { ggReleaseAPIBase = oldBase })
	ggReleaseAPIBase = srv.URL + "/latest"
	exePath := withStubbedExe(t)

	stdout, stderr, err := captureUpdateOutput(t, func() error {
		cmd := *updateCmd
		cmd.SetContext(context.Background())
		return runUpdate(&cmd, nil)
	})
	if err != nil {
		t.Fatalf("update failed: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "updated v0.3.15 → v0.3.16") {
		t.Fatalf("expected update to advertised latest v0.3.16, got:\n%s", stdout)
	}
	got, readErr := os.ReadFile(exePath)
	if readErr != nil {
		t.Fatalf("read replaced binary: %v", readErr)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("binary not replaced with latest release payload: %q", got)
	}
}
GO

trap 'rm -f cmd/update_bug060_repro_test.go' EXIT

go test ./cmd -run TestBUG060UpdateInstallsAdvertisedLatest -count=1
