package cmd

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/spf13/cobra"
)

type fakeSystemSyncClient struct {
	healthErr         error
	statusErr         error
	ensureErr         error
	present           []string
	missing           []string
	presentAfterRetry []string
	missingAfterRetry []string
	statusCalls       int
	ensureCalled      bool
	ensuredVectorSize uint64
}

func (f *fakeSystemSyncClient) HealthCheck(_ context.Context) error {
	return f.healthErr
}

func (f *fakeSystemSyncClient) CollectionStatus(_ context.Context) (present, missing []string, err error) {
	f.statusCalls++
	if f.statusErr != nil {
		return nil, nil, f.statusErr
	}
	if f.statusCalls > 1 && (f.presentAfterRetry != nil || f.missingAfterRetry != nil) {
		return f.presentAfterRetry, f.missingAfterRetry, nil
	}
	return f.present, f.missing, nil
}

func (f *fakeSystemSyncClient) EnsureCollections(_ context.Context, vectorSize uint64) error {
	f.ensureCalled = true
	f.ensuredVectorSize = vectorSize
	return f.ensureErr
}

func (f *fakeSystemSyncClient) Close() error {
	return nil
}

type systemSyncRunFixture struct {
	root string
	id   string
}

func newSystemSyncRunFixture(t *testing.T, withGGDir bool) *systemSyncRunFixture {
	t.Helper()
	root := t.TempDir()
	if withGGDir {
		ggDir := filepath.Join(root, config.DirName)
		if err := os.MkdirAll(ggDir, 0o755); err != nil {
			t.Fatalf("mkdir .gg: %v", err)
		}
		cfg := "project_id: " + filepath.Base(root) + "\n" +
			"qdrant:\n  host: 127.0.0.1\n  port: 19997\n" +
			"embedding:\n  host: http://localhost:11434\n  model: nomic-embed-text\n" +
			"memgraph:\n  uri: bolt://localhost:1\n"
		if err := os.WriteFile(filepath.Join(ggDir, "config.yaml"), []byte(cfg), 0o644); err != nil {
			t.Fatalf("write config.yaml: %v", err)
		}
	}
	return &systemSyncRunFixture{root: root, id: filepath.Base(root)}
}

func writeProjectRegistry(t *testing.T, home string, entries ...systemSyncRunFixture) {
	t.Helper()
	reg := `{"projects":{` + strings.Join(func() []string {
		parts := make([]string, 0, len(entries))
		for _, e := range entries {
			parts = append(parts, `"`+e.id+`":{"id":"`+e.id+`","root":"`+e.root+`","name":"`+e.id+`","registered_at":"2026-01-01T00:00:00Z"}`)
		}
		return parts
	}(), ",") + `}}`
	ggPath := filepath.Join(home, ".gg", config.RegistryFile)
	if err := os.MkdirAll(filepath.Dir(ggPath), 0o755); err != nil {
		t.Fatalf("mkdir shared state: %v", err)
	}
	if err := os.WriteFile(ggPath, []byte(reg), 0o644); err != nil {
		t.Fatalf("write projects.json: %v", err)
	}
}

func TestSystemSync_TrackerMissingCollections_EnsuresAndContinuesToDoctor(t *testing.T) {
	setupGGDir(t)
	home, _ := os.UserHomeDir()

	proj := newSystemSyncRunFixture(t, true)
	writeProjectRegistry(t, home, *proj)

	trackerClient := &fakeSystemSyncClient{present: []string{"existing"}, missing: []string{"missing-1", "missing-2"}}
	oldClient := systemSyncNewQdrantClient
	oldRun := systemSyncRunCommand
	runCount := 0
	systemSyncNewQdrantClient = func(cfg *config.Config, _ string) (systemSyncQdrant, error) {
		return trackerClient, nil
	}
	systemSyncRunCommand = func(_, _ string, _ ...string) error {
		runCount++
		return nil
	}
	t.Cleanup(func() {
		systemSyncNewQdrantClient = oldClient
		systemSyncRunCommand = oldRun
	})

	_, _, err := execCmd(t, "system", "sync")
	if err != nil {
		t.Fatalf("system sync failed: %v", err)
	}
	if trackerClient.statusCalls == 0 || !trackerClient.ensureCalled {
		t.Fatal("expected tracker status and ensure to run")
	}
	if runCount != 3 {
		t.Fatalf("expected 3 doctor runs (contract + 2 hook stages), got %d", runCount)
	}
}

func TestSystemSync_TrackerDownSkipsAndContinues(t *testing.T) {
	setupGGDir(t)
	home, _ := os.UserHomeDir()

	proj := newSystemSyncRunFixture(t, true)
	writeProjectRegistry(t, home, *proj)

	trackerClient := &fakeSystemSyncClient{healthErr: errors.New("qdrant unavailable")}
	oldClient := systemSyncNewQdrantClient
	oldRun := systemSyncRunCommand
	runCount := 0
	systemSyncNewQdrantClient = func(cfg *config.Config, _ string) (systemSyncQdrant, error) {
		return trackerClient, nil
	}
	systemSyncRunCommand = func(_, _ string, _ ...string) error {
		runCount++
		return nil
	}
	t.Cleanup(func() {
		systemSyncNewQdrantClient = oldClient
		systemSyncRunCommand = oldRun
	})

	_, _, err := execCmd(t, "system", "sync")
	if err != nil {
		t.Fatalf("system sync failed: %v", err)
	}
	if trackerClient.statusCalls > 0 || trackerClient.ensureCalled {
		t.Fatalf("health failure should skip collection status/ensure checks")
	}
	if runCount != 3 {
		t.Fatalf("expected sync stages to continue when tracker is unavailable, got %d", runCount)
	}
}

func TestSystemSync_MissingRootOrConfigSkipsDistinctly(t *testing.T) {
	setupGGDir(t)
	home, _ := os.UserHomeDir()

	missingRoot := filepath.Join(t.TempDir(), "gone")
	withConfig := newSystemSyncRunFixture(t, false)
	if err := os.Mkdir(filepath.Join(withConfig.root, ".gg"), 0o755); err != nil {
		t.Fatalf("mkdir .gg: %v", err)
	}

	writeProjectRegistry(
		t, home,
		systemSyncRunFixture{root: missingRoot, id: "missing-root"},
		systemSyncRunFixture{root: withConfig.root, id: withConfig.id},
	)

	oldClient := systemSyncNewQdrantClient
	oldRun := systemSyncRunCommand
	runCount := 0
	systemSyncNewQdrantClient = func(cfg *config.Config, _ string) (systemSyncQdrant, error) {
		t.Fatalf("qdrant client should not be used")
		return nil, nil
	}
	systemSyncRunCommand = func(_, _ string, _ ...string) error {
		runCount++
		return nil
	}
	t.Cleanup(func() {
		systemSyncNewQdrantClient = oldClient
		systemSyncRunCommand = oldRun
	})

	var err error
	out := captureStdout(t, func() {
		_, _, err = execCmd(t, "system", "sync")
	})
	if err != nil {
		t.Fatalf("system sync failed: %v", err)
	}
	if !strings.Contains(out, "project root missing") || !strings.Contains(out, ".gg/config.yaml missing") {
		t.Fatalf("expected distinct missing root/config output, got:\n%s", out)
	}
	if runCount != 0 {
		t.Fatalf("expected no doctor runs for skipped projects, got %d", runCount)
	}
}

func TestSystemSync_TrackerUsesEmbeddingMetaVectorSize(t *testing.T) {
	setupGGDir(t)
	home, _ := os.UserHomeDir()

	proj := newSystemSyncRunFixture(t, true)
	meta := `{"model_name":"custom","dim":1536,"created_at":"2026-01-01T00:00:00Z"}`
	if err := os.WriteFile(filepath.Join(proj.root, config.DirName, "embedding-meta.json"), []byte(meta), 0o644); err != nil {
		t.Fatalf("write embedding meta: %v", err)
	}
	writeProjectRegistry(t, home, *proj)

	trackerClient := &fakeSystemSyncClient{missing: []string{"missing"}}
	oldClient := systemSyncNewQdrantClient
	oldRun := systemSyncRunCommand
	systemSyncNewQdrantClient = func(*config.Config, string) (systemSyncQdrant, error) {
		return trackerClient, nil
	}
	systemSyncRunCommand = func(_, _ string, _ ...string) error { return nil }
	t.Cleanup(func() {
		systemSyncNewQdrantClient = oldClient
		systemSyncRunCommand = oldRun
	})

	_, _, err := execCmd(t, "system", "sync")
	if err != nil {
		t.Fatalf("system sync failed: %v", err)
	}
	if trackerClient.ensuredVectorSize != 1536 {
		t.Fatalf("expected EnsureCollections vector size 1536, got %d", trackerClient.ensuredVectorSize)
	}
}

func TestSystemSync_TrackerStatusUnavailableSkipsAndContinues(t *testing.T) {
	setupGGDir(t)
	home, _ := os.UserHomeDir()

	proj := newSystemSyncRunFixture(t, true)
	writeProjectRegistry(t, home, *proj)

	trackerClient := &fakeSystemSyncClient{statusErr: errors.New("connection refused")}
	oldClient := systemSyncNewQdrantClient
	oldRun := systemSyncRunCommand
	runCount := 0
	systemSyncNewQdrantClient = func(*config.Config, string) (systemSyncQdrant, error) {
		return trackerClient, nil
	}
	systemSyncRunCommand = func(_, _ string, _ ...string) error {
		runCount++
		return nil
	}
	t.Cleanup(func() {
		systemSyncNewQdrantClient = oldClient
		systemSyncRunCommand = oldRun
	})

	_, _, err := execCmd(t, "system", "sync")
	if err != nil {
		t.Fatalf("system sync failed: %v", err)
	}
	if trackerClient.ensureCalled {
		t.Fatal("status failure should skip ensure")
	}
	if runCount != 3 {
		t.Fatalf("expected doctor stages to continue, got %d", runCount)
	}
}

func TestSystemSync_TrackerHealthContextCanceledIsNotQdrantUnavailable(t *testing.T) {
	setupGGDir(t)
	home, _ := os.UserHomeDir()

	proj := newSystemSyncRunFixture(t, true)
	writeProjectRegistry(t, home, *proj)

	trackerClient := &fakeSystemSyncClient{healthErr: context.Canceled}
	oldClient := systemSyncNewQdrantClient
	oldRun := systemSyncRunCommand
	runCount := 0
	systemSyncNewQdrantClient = func(*config.Config, string) (systemSyncQdrant, error) {
		return trackerClient, nil
	}
	systemSyncRunCommand = func(_, _ string, _ ...string) error {
		runCount++
		return nil
	}
	t.Cleanup(func() {
		systemSyncNewQdrantClient = oldClient
		systemSyncRunCommand = oldRun
	})

	_, _, err := execCmd(t, "system", "sync")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if runCount != 0 {
		t.Fatalf("context.Canceled should not continue to doctor stages, got %d", runCount)
	}
}

func TestSystemSync_TrackerAlreadyExistsRaceRechecksCollections(t *testing.T) {
	setupGGDir(t)
	home, _ := os.UserHomeDir()

	proj := newSystemSyncRunFixture(t, true)
	writeProjectRegistry(t, home, *proj)

	trackerClient := &fakeSystemSyncClient{
		missing:           []string{"missing"},
		ensureErr:         errors.New("already exists"),
		presentAfterRetry: []string{"all"},
		missingAfterRetry: []string{},
	}
	oldClient := systemSyncNewQdrantClient
	oldRun := systemSyncRunCommand
	runCount := 0
	systemSyncNewQdrantClient = func(*config.Config, string) (systemSyncQdrant, error) {
		return trackerClient, nil
	}
	systemSyncRunCommand = func(_, _ string, _ ...string) error {
		runCount++
		return nil
	}
	t.Cleanup(func() {
		systemSyncNewQdrantClient = oldClient
		systemSyncRunCommand = oldRun
	})

	_, _, err := execCmd(t, "system", "sync")
	if err != nil {
		t.Fatalf("system sync failed: %v", err)
	}
	if trackerClient.statusCalls != 2 {
		t.Fatalf("expected status recheck after already-exists race, got %d", trackerClient.statusCalls)
	}
	if runCount != 3 {
		t.Fatalf("expected doctor stages to continue, got %d", runCount)
	}
}

func TestSystemSync_DryRunDoesNotRunDoctorOrTracker(t *testing.T) {
	setupGGDir(t)
	home, _ := os.UserHomeDir()

	proj := newSystemSyncRunFixture(t, true)
	writeProjectRegistry(t, home, *proj)

	oldClient := systemSyncNewQdrantClient
	oldRun := systemSyncRunCommand
	systemSyncNewQdrantClient = func(_ *config.Config, _ string) (systemSyncQdrant, error) {
		t.Fatalf("dry-run should not create qdrant client")
		return nil, nil
	}
	systemSyncRunCommand = func(_, _ string, _ ...string) error {
		t.Fatalf("dry-run should not execute doctor stages")
		return nil
	}
	t.Cleanup(func() {
		systemSyncNewQdrantClient = oldClient
		systemSyncRunCommand = oldRun
	})

	var err error
	out := captureStdout(t, func() {
		_, _, err = execCmd(t, "system", "sync", "--dry-run")
	})
	if err != nil {
		t.Fatalf("system sync --dry-run failed: %v", err)
	}
	if !strings.Contains(out, "(dry-run) would check tracker collections") {
		t.Fatalf("dry-run output should include tracker self-heal stage, got:\n%s", out)
	}
}

func TestSystemSync_RootContextCanceledAbortsWithoutTrackerOrDoctor(t *testing.T) {
	setupGGDir(t)
	home, _ := os.UserHomeDir()

	proj := newSystemSyncRunFixture(t, true)
	writeProjectRegistry(t, home, *proj)

	oldClient := systemSyncNewQdrantClient
	oldRun := systemSyncRunCommand
	systemSyncNewQdrantClient = func(_ *config.Config, _ string) (systemSyncQdrant, error) {
		t.Fatalf("canceled sync should not create qdrant client")
		return nil, nil
	}
	systemSyncRunCommand = func(_, _ string, _ ...string) error {
		t.Fatalf("canceled sync should not execute doctor stages")
		return nil
	}
	t.Cleanup(func() {
		systemSyncNewQdrantClient = oldClient
		systemSyncRunCommand = oldRun
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cmd := &cobra.Command{}
	cmd.SetContext(ctx)

	if err := runSystemSync(cmd, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if systemSyncQdrantUnavailable(context.Canceled) {
		t.Fatal("context.Canceled must not be classified as qdrant unavailable")
	}
}

// TestSystemSync_RefreshesIndexHooksOnlyWhenPresent verifies the CodeGraph
// git-hook refresh stage runs for a project that already opted in (index hook
// present) and is skipped for a project that has none — so `gg system sync`
// propagates a detached-hook template update without force-installing index
// hooks into opt-out projects.
func TestSystemSync_RefreshesIndexHooksOnlyWhenPresent(t *testing.T) {
	setupGGDir(t)
	home, _ := os.UserHomeDir()

	withHooks := newSystemSyncRunFixture(t, true)
	hooksDir := filepath.Join(withHooks.root, ".git", "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hooksDir, "pre-push"), []byte(indexHookBody), 0o755); err != nil {
		t.Fatal(err)
	}
	without := newSystemSyncRunFixture(t, true)
	writeProjectRegistry(t, home, *withHooks, *without)

	trackerClient := &fakeSystemSyncClient{present: []string{"existing"}}
	oldClient := systemSyncNewQdrantClient
	oldRun := systemSyncRunCommand
	indexRefreshRoots := map[string]bool{}
	systemSyncNewQdrantClient = func(cfg *config.Config, _ string) (systemSyncQdrant, error) {
		return trackerClient, nil
	}
	systemSyncRunCommand = func(_, dir string, args ...string) error {
		if len(args) >= 2 && args[0] == "doctor" && args[1] == "--install-index-hooks" {
			indexRefreshRoots[dir] = true
		}
		return nil
	}
	t.Cleanup(func() {
		systemSyncNewQdrantClient = oldClient
		systemSyncRunCommand = oldRun
	})

	if _, _, err := execCmd(t, "system", "sync"); err != nil {
		t.Fatalf("system sync failed: %v", err)
	}
	if !indexRefreshRoots[withHooks.root] {
		t.Errorf("index-hook refresh should run for opted-in project %s", withHooks.root)
	}
	if indexRefreshRoots[without.root] {
		t.Errorf("index-hook refresh must NOT run for opt-out project %s", without.root)
	}
}
