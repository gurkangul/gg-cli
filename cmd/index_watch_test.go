package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestIndexWatchLockPreventsSecondWatcher(t *testing.T) {
	ggDir := t.TempDir()
	release, err := acquireIndexWatchLock(ggDir, indexWatchLock{
		PID:       os.Getpid(),
		StartedAt: time.Now().UTC().Format(time.RFC3339),
		Lang:      "go",
		Root:      t.TempDir(),
	})
	if err != nil {
		t.Fatalf("acquire first lock: %v", err)
	}
	defer release()

	_, err = acquireIndexWatchLock(ggDir, indexWatchLock{PID: os.Getpid(), Lang: "go"})
	if err == nil || !strings.Contains(err.Error(), "already running") {
		t.Fatalf("expected already running error, got %v", err)
	}
}

func TestIndexWatchLockReleaseRemovesFile(t *testing.T) {
	ggDir := t.TempDir()
	release, err := acquireIndexWatchLock(ggDir, indexWatchLock{PID: os.Getpid(), Lang: "go"})
	if err != nil {
		t.Fatalf("acquire lock: %v", err)
	}
	release()
	if _, err := os.Stat(filepath.Join(ggDir, indexWatchLockFile)); !os.IsNotExist(err) {
		t.Fatalf("lock should be removed, got %v", err)
	}
}

func TestCodeGraphStatusShowsRunningWatcher(t *testing.T) {
	ggDir := t.TempDir()
	release, err := acquireIndexWatchLock(ggDir, indexWatchLock{
		PID:       os.Getpid(),
		StartedAt: "2026-05-03T00:00:00Z",
		Lang:      "go",
	})
	if err != nil {
		t.Fatalf("acquire lock: %v", err)
	}
	defer release()

	status := codeGraphStatus{NoWatcherStarted: true}
	status.fillWatcher(ggDir)
	if status.NoWatcherStarted {
		t.Fatal("watcher should be marked as running")
	}
	if !strings.Contains(status.Watcher, "lang=go") {
		t.Fatalf("unexpected watcher detail: %q", status.Watcher)
	}
}
