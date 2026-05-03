package config

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestLinkedProjectConfigUnmarshalScalarIDAndPath(t *testing.T) {
	var cfg struct {
		Linked []LinkedProjectConfig `yaml:"linked_projects"`
	}
	input := []byte(`
linked_projects:
  - 11111111-1111-1111-1111-111111111111
  - ../other-project
  - project_id: 22222222-2222-2222-2222-222222222222
  - path: ~/src/linked
`)
	if err := yaml.Unmarshal(input, &cfg); err != nil {
		t.Fatalf("unmarshal linked projects: %v", err)
	}
	if got := cfg.Linked[0].ProjectID; got != "11111111-1111-1111-1111-111111111111" {
		t.Fatalf("scalar id: got %q", got)
	}
	if got := cfg.Linked[1].Path; got != "../other-project" {
		t.Fatalf("scalar path: got %q", got)
	}
	if got := cfg.Linked[2].ProjectID; got != "22222222-2222-2222-2222-222222222222" {
		t.Fatalf("mapping id: got %q", got)
	}
	if got := cfg.Linked[3].Path; got != "~/src/linked" {
		t.Fatalf("mapping path: got %q", got)
	}
}

func TestResolveLinkedProjectFromPathReadsProjectConfig(t *testing.T) {
	root := t.TempDir()
	ggDir := filepath.Join(root, DirName)
	if err := makeProjectConfig(ggDir, "33333333-3333-3333-3333-333333333333"); err != nil {
		t.Fatalf("make config: %v", err)
	}

	projectID, resolvedGGDir, err := ResolveLinkedProject(LinkedProjectConfig{Path: root})
	if err != nil {
		t.Fatalf("resolve linked path: %v", err)
	}
	if projectID != "33333333-3333-3333-3333-333333333333" {
		t.Fatalf("project id: got %q", projectID)
	}
	if resolvedGGDir != ggDir {
		t.Fatalf("gg dir: got %q, want %q", resolvedGGDir, ggDir)
	}
}

func makeProjectConfig(ggDir, projectID string) error {
	if err := os.MkdirAll(ggDir, 0o755); err != nil {
		return err
	}
	data := []byte(`project_id: ` + projectID + `
qdrant:
  host: localhost
  port: 6334
embedding:
  host: http://localhost:11434
  model: nomic-embed-text
memgraph:
  uri: bolt://localhost:7687
`)
	return os.WriteFile(filepath.Join(ggDir, ConfigFile), data, 0o644)
}
