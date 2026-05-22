package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/graph"
	"github.com/gurkangul/gg-cli/internal/index/runner"
)

func TestShouldUseFullIndexForFix(t *testing.T) {
	cases := []struct {
		name   string
		status codeGraphStatus
		want   bool
	}{
		{"missing graph empty", codeGraphStatus{Status: "missing", GraphEmpty: true}, true},
		{"missing memgraph unavailable", codeGraphStatus{Status: "missing", MemgraphDetail: "unavailable"}, true},
		{"missing memgraph not checked", codeGraphStatus{Status: "missing", MemgraphDetail: "not checked"}, true},
		{"missing stats unavailable", codeGraphStatus{Status: "missing", MemgraphAvailable: true, GraphStatsAvailable: false}, true},
		{"missing other", codeGraphStatus{Status: "missing", MemgraphAvailable: true, GraphStatsAvailable: true, MemgraphDetail: "reachable"}, false},
		{"stale graph empty", codeGraphStatus{Status: "stale", GraphEmpty: true}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := shouldUseFullIndexForFix(tc.status)
			if got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestDoctorFixIndex_RepopulatesEmptyMemgraphProjection(t *testing.T) {
	if os.Getenv("GG_INTEGRATION_TEST") != "1" {
		t.Skip("set GG_INTEGRATION_TEST=1 to run Memgraph integration tests")
	}
	if _, err := exec.LookPath("scip-go"); err != nil {
		t.Skipf("scip-go not available: %v", err)
	}

	memgraphURI := os.Getenv("MEMGRAPH_URI")
	if memgraphURI == "" {
		memgraphURI = "bolt://localhost:7687"
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	root, err := os.MkdirTemp(cwd, ".doctor-fix-index-")
	if err != nil {
		t.Fatalf("mkdir temp project: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	home := t.TempDir()
	ggDir := filepath.Join(root, config.DirName)
	if err := os.MkdirAll(ggDir, 0o755); err != nil {
		t.Fatalf("mkdir .gg: %v", err)
	}
	projectID := fmt.Sprintf("doctor-fix-index-%d", time.Now().UnixNano())
	cfgText := fmt.Sprintf(`project_id: %s
qdrant:
  host: "127.0.0.1"
  port: 19997
embedding:
  host: "http://localhost:11434"
  model: "nomic-embed-text"
memgraph:
  uri: %q
`, projectID, memgraphURI)
	if err := os.WriteFile(filepath.Join(ggDir, "config.yaml"), []byte(cfgText), 0o644); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}
	t.Setenv("HOME", home)
	t.Setenv("GG_AGENT", "test")
	t.Setenv("GG_ROLE", "")
	t.Setenv("GOWORK", "off")
	t.Chdir(root)

	git(t, root, "init")
	git(t, root, "config", "user.email", "test@example.com")
	git(t, root, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/doctorfix\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}
	git(t, root, "add", ".")
	git(t, root, "commit", "-m", "initial")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	gc, err := graph.New(&cfg.Memgraph, cfg.ProjectID)
	if err != nil {
		t.Fatalf("graph.New: %v", err)
	}
	ctx := context.Background()
	if err := gc.HealthCheck(ctx); err != nil {
		_ = gc.Close(ctx)
		t.Skipf("Memgraph not reachable: %v", err)
	}
	defer func() {
		_ = gc.SweepProject(ctx)
		_ = gc.Close(ctx)
	}()

	cmd := &cobra.Command{}
	cmd.SetContext(ctx)
	if err := runIndexOnce(cmd, runner.LangGo, false); err != nil {
		t.Fatalf("initial full index: %v", err)
	}
	if err := gc.SweepProject(ctx); err != nil {
		t.Fatalf("sweep project graph: %v", err)
	}

	status := collectCodeGraphStatus(ctx, root, ggDir, cfg)
	if status.Status != "missing" || !status.GraphEmpty {
		t.Fatalf("pre-fix status=%q graphEmpty=%v detail=%q", status.Status, status.GraphEmpty, status.Detail)
	}
	if status.SuggestedCommand != "gg index --lang go" || strings.Contains(status.Detail, "--changed") {
		t.Fatalf("empty graph should suggest full index, detail=%q suggested=%q", status.Detail, status.SuggestedCommand)
	}

	if err := runDoctorFixIndex(cmd); err != nil {
		t.Fatalf("runDoctorFixIndex: %v", err)
	}
	after := collectCodeGraphStatus(ctx, root, ggDir, cfg)
	if after.Status != "ready" {
		t.Fatalf("after status=%q detail=%q stats=%+v", after.Status, after.Detail, after.Stats)
	}
	if after.Stats.Files == 0 || after.Stats.Symbols == 0 {
		t.Fatalf("expected repopulated graph stats, got %+v", after.Stats)
	}
}
