package cmd

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/store"
)

func TestSystemBrainStatusCommandSeparatesContractSyncFromBrainHealth(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	projectRoot := writeSystemBrainProject(t, "proj-a", "proj-a", false, "")
	writeSystemBrainRegistry(t, home, map[string]string{"proj-a": projectRoot})
	t.Chdir(t.TempDir())

	stdout, stderr, err := execCmd(t, "system", "brain", "status")
	if err != nil {
		t.Fatalf("system brain status returned error: %v\nstderr=%s\nstdout=%s", err, stderr, stdout)
	}
	for _, want := range []string{
		"Brain health across 1 registered project(s)",
		"Note: gg system sync refreshes contracts/hooks plus tracker self-heal; brain status reports snapshot drift, portability checks, and CodeGraph health.",
		"proj-a",
		"registry=ok",
		"project_id=ok",
		"snapshot=none",
		"qdrant=down",
		"ollama=down",
		"codegraph=not_applicable",
		"registry_issues=0",
		"drifted_snapshot=0",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("output missing %q\nstdout:\n%s", want, stdout)
		}
	}
}

func TestSystemBrainStatusJSONReportsMismatchAndFreshSnapshot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	projectRoot := writeSystemBrainProject(t, "actual-project", "actual-project", true, time.Now().UTC().Format(time.RFC3339))
	writeSystemBrainRegistry(t, home, map[string]string{"registry-project": projectRoot})
	t.Chdir(t.TempDir())

	stdout, stderr, err := execCmd(t, "--json", "system", "brain", "status")
	if err != nil {
		t.Fatalf("system brain status --json returned error: %v\nstderr=%s\nstdout=%s", err, stderr, stdout)
	}
	var payload struct {
		Summary struct {
			Total    int `json:"total"`
			Healthy  int `json:"healthy"`
			Problems int `json:"problems"`
		} `json:"summary"`
		Projects []struct {
			Name            string `json:"name"`
			RegistryID      string `json:"registry_id"`
			ConfigProjectID string `json:"config_project_id"`
			RegistryStatus  string `json:"registry_status"`
			ProjectIDStatus string `json:"project_id_status"`
			Snapshot        struct {
				Status    string `json:"status"`
				Checksums string `json:"checksums"`
			} `json:"snapshot"`
			Qdrant struct {
				Status string `json:"status"`
			} `json:"qdrant"`
			Ollama struct {
				Status string `json:"status"`
			} `json:"ollama"`
			CodeGraph struct {
				Status string `json:"status"`
			} `json:"codegraph"`
		} `json:"projects"`
	}
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("parse json: %v\nstdout:\n%s", err, stdout)
	}
	if payload.Summary.Total != 1 || payload.Summary.Healthy != 0 || payload.Summary.Problems != 1 {
		t.Fatalf("summary mismatch: %#v", payload.Summary)
	}
	if len(payload.Projects) != 1 {
		t.Fatalf("projects len=%d", len(payload.Projects))
	}
	got := payload.Projects[0]
	if got.RegistryID != "registry-project" || got.ConfigProjectID != "actual-project" {
		t.Fatalf("project ids mismatch: %#v", got)
	}
	if got.RegistryStatus != "ok" || got.ProjectIDStatus != "mismatch" {
		t.Fatalf("registry/project id status mismatch: %#v", got)
	}
	if got.Snapshot.Status != "fresh" || got.Snapshot.Checksums != "ok" {
		t.Fatalf("snapshot mismatch: %#v", got.Snapshot)
	}
	if got.Qdrant.Status != "down" || got.Ollama.Status != "down" || got.CodeGraph.Status != "not_applicable" {
		t.Fatalf("backend/codegraph status mismatch: qdrant=%#v ollama=%#v codegraph=%#v", got.Qdrant, got.Ollama, got.CodeGraph)
	}
}

func TestSystemBrainStatusJSONReportsDriftedSnapshot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	projectRoot := writeSystemBrainProject(t, "proj-a", "proj-a", true, time.Now().UTC().Format(time.RFC3339))
	writeSystemBrainRegistry(t, home, map[string]string{"proj-a": projectRoot})
	t.Chdir(t.TempDir())

	originalClient := systemBrainNewQdrantClient
	systemBrainNewQdrantClient = func(_ *config.Config, _ string) (systemBrainQdrantClient, error) {
		return fakeSystemBrainQdrantClient{recordsByKind: map[string][]store.BrainRecord{
			store.BrainKind[0]: {{ID: "live-only"}},
		}}, nil
	}
	t.Cleanup(func() { systemBrainNewQdrantClient = originalClient })

	stdout, stderr, err := execCmd(t, "--json", "system", "brain", "status")
	if err != nil {
		t.Fatalf("system brain status --json returned error: %v\nstderr=%s\nstdout=%s", err, stderr, stdout)
	}
	var payload struct {
		Summary struct {
			DriftedSnapshot int `json:"drifted_snapshot"`
		} `json:"summary"`
		Projects []struct {
			Snapshot struct {
				Status string         `json:"status"`
				Drift  string         `json:"drift"`
				Delta  map[string]int `json:"drift_delta"`
			} `json:"snapshot"`
		} `json:"projects"`
	}
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("parse json: %v\nstdout:\n%s", err, stdout)
	}
	if payload.Summary.DriftedSnapshot != 1 {
		t.Fatalf("drifted summary mismatch: %#v", payload.Summary)
	}
	if got := payload.Projects[0].Snapshot; got.Status != "drifted" || got.Drift != "drifted" || got.Delta[store.BrainKind[0]] != 1 {
		t.Fatalf("snapshot drift mismatch: %#v", got)
	}
}

func TestSystemBrainStatusFlagsInvalidSnapshotManifest(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	projectRoot := writeSystemBrainProject(t, "proj-a", "proj-a", true, time.Now().UTC().Format(time.RFC3339))
	writeSystemBrainRegistry(t, home, map[string]string{"proj-a": projectRoot})
	t.Chdir(t.TempDir())

	manifest := brainManifest{
		SchemaVersion: 1,
		GGVersion:     "test",
		ProjectID:     "other-project",
		ExportedAt:    time.Now().UTC().Format(time.RFC3339),
		Counts:        map[string]int{},
		SHA256:        map[string]string{},
	}
	data, err := brainMarshalManifest(manifest)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectRoot, ".gg", brainDirName, "manifest.json"), append(data, '\n'), 0o644); err != nil {
		t.Fatalf("overwrite manifest: %v", err)
	}

	stdout, stderr, err := execCmd(t, "--json", "system", "brain", "status")
	if err != nil {
		t.Fatalf("system brain status --json returned error: %v\nstderr=%s\nstdout=%s", err, stderr, stdout)
	}
	var payload struct {
		Projects []struct {
			Snapshot struct {
				Status string `json:"status"`
				Detail string `json:"detail"`
			} `json:"snapshot"`
		} `json:"projects"`
	}
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("parse json: %v\nstdout:\n%s", err, stdout)
	}
	got := payload.Projects[0].Snapshot
	if got.Status != "unknown" || got.Detail != "snapshot project_id mismatch" {
		t.Fatalf("snapshot mismatch not surfaced: %#v", got)
	}
}

func TestBrainManifestChecksumsRejectsEscapingFilenames(t *testing.T) {
	manifest := brainManifest{SHA256: map[string]string{"../outside.jsonl": "bad"}}
	if brainManifestChecksumsOK(t.TempDir(), manifest) {
		t.Fatal("expected escaping checksum filename to fail")
	}
}

func TestSystemBrainStatusEmptyRegistry(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeSystemBrainRegistry(t, home, map[string]string{})
	t.Chdir(t.TempDir())

	stdout, stderr, err := execCmd(t, "system", "brain", "status")
	if err != nil {
		t.Fatalf("system brain status returned error: %v\nstderr=%s\nstdout=%s", err, stderr, stdout)
	}
	for _, want := range []string{
		"Brain health across 0 registered project(s)",
		"No projects registered.",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("output missing %q\nstdout:\n%s", want, stdout)
		}
	}
}

type fakeSystemBrainQdrantClient struct {
	recordsByKind map[string][]store.BrainRecord
}

func (fakeSystemBrainQdrantClient) HealthCheck(context.Context) error { return nil }

func (fakeSystemBrainQdrantClient) Close() error { return nil }

func (f fakeSystemBrainQdrantClient) ExportBrainCollection(_ context.Context, kind string) ([]store.BrainRecord, error) {
	return f.recordsByKind[kind], nil
}

func writeSystemBrainProject(t *testing.T, projectID, name string, withSnapshot bool, exportedAt string) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), name)
	ggDir := filepath.Join(root, ".gg")
	if err := os.MkdirAll(ggDir, 0o755); err != nil {
		t.Fatalf("mkdir .gg: %v", err)
	}
	cfg := `project_id: ` + projectID + `
qdrant:
  host: "127.0.0.1"
  port: 19997
embedding:
  host: "http://127.0.0.1:19998"
  model: "nomic-embed-text"
memgraph:
  uri: "bolt://127.0.0.1:1"
`
	if err := os.WriteFile(filepath.Join(ggDir, "config.yaml"), []byte(cfg), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if withSnapshot {
		brainDir := filepath.Join(ggDir, brainDirName)
		if err := os.MkdirAll(brainDir, 0o755); err != nil {
			t.Fatalf("mkdir brain: %v", err)
		}
		manifest := brainManifest{
			SchemaVersion: 1,
			GGVersion:     "test",
			ProjectID:     projectID,
			ExportedAt:    exportedAt,
			Counts:        map[string]int{},
			SHA256:        map[string]string{},
		}
		data, err := brainMarshalManifest(manifest)
		if err != nil {
			t.Fatalf("marshal manifest: %v", err)
		}
		if err := os.WriteFile(filepath.Join(brainDir, "manifest.json"), append(data, '\n'), 0o644); err != nil {
			t.Fatalf("write manifest: %v", err)
		}
	}
	return root
}

func writeSystemBrainRegistry(t *testing.T, home string, projects map[string]string) {
	t.Helper()
	type entry struct {
		ID           string `json:"id"`
		Root         string `json:"root"`
		Name         string `json:"name"`
		RegisteredAt string `json:"registered_at"`
	}
	payload := struct {
		Projects map[string]entry `json:"projects"`
	}{Projects: map[string]entry{}}
	for id, root := range projects {
		payload.Projects[id] = entry{
			ID:           id,
			Root:         root,
			Name:         filepath.Base(root),
			RegisteredAt: "2026-05-18T00:00:00Z",
		}
	}
	registryDir := filepath.Join(home, ".gg")
	if err := os.MkdirAll(registryDir, 0o755); err != nil {
		t.Fatalf("mkdir registry dir: %v", err)
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		t.Fatalf("marshal registry: %v", err)
	}
	if err := os.WriteFile(filepath.Join(registryDir, "projects.json"), append(data, '\n'), 0o644); err != nil {
		t.Fatalf("write registry: %v", err)
	}
}
