package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gurkangul/gg-cli/internal/config"
)

func TestRenderNext_WithCodeGraphWarningShowsRecommendation(t *testing.T) {
	var buf bytes.Buffer
	renderNext(&buf, nextSnapshot{
		Agent:         "agent-1",
		Role:          "implementer",
		StateWarnings: []string{"code graph missing/stale (missing): project gained code since init; detected language(s): go - run gg index --lang go --changed; recommended: gg index --lang go --changed"},
	})
	out := buf.String()
	for _, want := range []string{"warning: code graph missing/stale", "gg index --lang go --changed", "Recommended next step:"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

func TestCodeGraphActionWarning_PrefixesRecommendedCommand(t *testing.T) {
	if got := codeGraphAgentWarning(codeGraphStatus{Status: "missing", Detail: "project gained code since init; detected language(s): go", SuggestedCommand: "gg index --lang go --changed"}); !strings.Contains(got, "recommended: gg index --lang go --changed") {
		t.Fatalf("warning=%q", got)
	}
}

func TestEmitCodeGraphNotice_UsesWarningPath(t *testing.T) {
	setupGGDir(t)
	git(t, ".", "init")
	git(t, ".", "config", "user.email", "test@example.com")
	git(t, ".", "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(".", "package.json"), []byte("{\"name\":\"example\"}\n"), 0o644); err != nil {
		t.Fatalf("write package.json: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(".", "src"), 0o755); err != nil {
		t.Fatalf("mkdir src: %v", err)
	}
	if err := os.WriteFile(filepath.Join(".", "src", "index.ts"), []byte("export const value = 1\n"), 0o644); err != nil {
		t.Fatalf("write index.ts: %v", err)
	}

	loadedCfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	var buf bytes.Buffer
	emitCodeGraphNotice(context.Background(), &buf, loadedCfg)
	out := buf.String()
	for _, want := range []string{"CODE GRAPH NOTICE", "gg index --lang typescript --changed", "gg doctor --fix-index"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}
