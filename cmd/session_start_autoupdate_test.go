package cmd

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

// stubAutoUpdateStateDir points the throttle file at a temp dir so tests never
// touch the real ~/.gg.
func stubAutoUpdateStateDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	old := autoUpdateStateDir
	t.Cleanup(func() { autoUpdateStateDir = old })
	autoUpdateStateDir = func() (string, error) { return dir, nil }
	return dir
}

// failingReleaseAPI points ggReleaseAPIBase at a server that fails the request
// if hit, so a test can assert the network is NOT contacted.
func failingReleaseAPI(t *testing.T) *int {
	t.Helper()
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		http.Error(w, "should not be called", http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)
	old := ggReleaseAPIBase
	t.Cleanup(func() { ggReleaseAPIBase = old })
	ggReleaseAPIBase = srv.URL
	return &hits
}

func newAutoUpdateCmd() *cobra.Command {
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	return cmd
}

func TestAutoUpdate_SkipOnDevBuild(t *testing.T) {
	oldVer := version
	version = "dev-abc1234"
	t.Cleanup(func() { version = oldVer })

	stubAutoUpdateStateDir(t)
	hits := failingReleaseAPI(t)
	// Ensure opt-outs are not set.
	t.Setenv("GG_NO_AUTO_UPDATE", "")
	t.Setenv("GG_PIN_VERSION", "")

	var out, errBuf bytes.Buffer
	autoUpdateIfDue(newAutoUpdateCmd(), &out, &errBuf)
	if *hits != 0 {
		t.Fatalf("dev build hit the network %d times, want 0", *hits)
	}
	if out.Len() != 0 {
		t.Fatalf("dev build produced output: %q", out.String())
	}
}

func TestAutoUpdate_SkipWhenDisabled(t *testing.T) {
	oldVer := version
	version = "v2.3.0" // a clean release build (would otherwise check)
	t.Cleanup(func() { version = oldVer })
	stubAutoUpdateStateDir(t)
	hits := failingReleaseAPI(t)

	t.Setenv("GG_PIN_VERSION", "")
	t.Setenv("GG_NO_AUTO_UPDATE", "1")
	var out, errBuf bytes.Buffer
	autoUpdateIfDue(newAutoUpdateCmd(), &out, &errBuf)
	if *hits != 0 {
		t.Fatalf("GG_NO_AUTO_UPDATE=1 hit the network %d times, want 0", *hits)
	}
}

func TestAutoUpdate_SkipWhenPinned(t *testing.T) {
	oldVer := version
	version = "v2.3.0"
	t.Cleanup(func() { version = oldVer })
	stubAutoUpdateStateDir(t)
	hits := failingReleaseAPI(t)

	t.Setenv("GG_NO_AUTO_UPDATE", "")
	t.Setenv("GG_PIN_VERSION", "v2.3.0")
	var out, errBuf bytes.Buffer
	autoUpdateIfDue(newAutoUpdateCmd(), &out, &errBuf)
	if *hits != 0 {
		t.Fatalf("GG_PIN_VERSION set hit the network %d times, want 0", *hits)
	}
}

func TestAutoUpdate_ThrottleSkipsRecentCheck(t *testing.T) {
	oldVer := version
	version = "v2.3.0"
	t.Cleanup(func() { version = oldVer })
	t.Setenv("GG_NO_AUTO_UPDATE", "")
	t.Setenv("GG_PIN_VERSION", "")

	dir := stubAutoUpdateStateDir(t)
	hits := failingReleaseAPI(t)

	// Write a last-check 1h ago — inside the 24h window.
	now := time.Now()
	statePath := filepath.Join(dir, autoUpdateCheckFile)
	if err := writeAutoUpdateState(statePath, now.Add(-1*time.Hour)); err != nil {
		t.Fatal(err)
	}
	oldNow := autoUpdateNow
	t.Cleanup(func() { autoUpdateNow = oldNow })
	autoUpdateNow = func() time.Time { return now }

	var out, errBuf bytes.Buffer
	autoUpdateIfDue(newAutoUpdateCmd(), &out, &errBuf)
	if *hits != 0 {
		t.Fatalf("throttled run hit the network %d times, want 0", *hits)
	}
}

func TestAutoUpdate_DueAfterWindowWritesTimestamp(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("self-update not supported on windows")
	}
	oldVer := version
	version = "v2.3.3" // current == latest, so it checks but does not update
	t.Cleanup(func() { version = oldVer })
	t.Setenv("GG_NO_AUTO_UPDATE", "")
	t.Setenv("GG_PIN_VERSION", "")

	dir := stubAutoUpdateStateDir(t)

	// Stub release API returning the same version (no update available).
	payload := []byte("x")
	archive := makeReleaseArchive(t, payload)
	srv, _ := releaseServer(t, "v2.3.3", archive, false)
	oldBase := ggReleaseAPIBase
	t.Cleanup(func() { ggReleaseAPIBase = oldBase })
	ggReleaseAPIBase = srv.URL + "/latest"

	now := time.Now()
	oldNow := autoUpdateNow
	t.Cleanup(func() { autoUpdateNow = oldNow })
	autoUpdateNow = func() time.Time { return now }

	statePath := filepath.Join(dir, autoUpdateCheckFile)
	// No prior state => due. Run.
	var out, errBuf bytes.Buffer
	autoUpdateIfDue(newAutoUpdateCmd(), &out, &errBuf)

	// Timestamp must be written even though no update happened.
	if autoUpdateDue(statePath, now) {
		t.Fatalf("expected throttle timestamp written after a no-op check")
	}
	if out.Len() != 0 {
		t.Fatalf("no-op check produced output: %q", out.String())
	}
}

func TestAutoUpdateDue(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, autoUpdateCheckFile)
	now := time.Now()

	// Missing file => due.
	if !autoUpdateDue(statePath, now) {
		t.Fatalf("missing state should be due")
	}
	// Fresh write => not due.
	if err := writeAutoUpdateState(statePath, now); err != nil {
		t.Fatal(err)
	}
	if autoUpdateDue(statePath, now) {
		t.Fatalf("just-written state should not be due")
	}
	// 25h later => due again.
	if !autoUpdateDue(statePath, now.Add(25*time.Hour)) {
		t.Fatalf("state older than 24h should be due")
	}
}
