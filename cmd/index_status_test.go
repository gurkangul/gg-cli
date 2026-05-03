package cmd

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gurkangul/gg-cli/internal/index/state"
	"github.com/gurkangul/gg-cli/internal/outbox"
)

func TestCollectCodeGraphStatus_ReadyAndStale(t *testing.T) {
	root, ggDir := setupIndexStatusRepo(t)
	first := gitCommit(t, root, "one.txt", "one")
	if err := state.Write(ggDir, first); err != nil {
		t.Fatalf("state.Write: %v", err)
	}

	ready := collectCodeGraphStatus(context.Background(), root, ggDir, nil)
	if ready.Status != "ready" {
		t.Fatalf("ready status=%q detail=%q", ready.Status, ready.Detail)
	}
	if ready.LastIndexedSHA != first || ready.HeadSHA != first {
		t.Fatalf("ready sha mismatch: %#v", ready)
	}

	second := gitCommit(t, root, "two.txt", "two")
	stale := collectCodeGraphStatus(context.Background(), root, ggDir, nil)
	if stale.Status != "stale" {
		t.Fatalf("stale status=%q detail=%q", stale.Status, stale.Detail)
	}
	if stale.LastIndexedSHA != first || stale.HeadSHA != second {
		t.Fatalf("stale sha mismatch: %#v", stale)
	}
}

func TestCollectCodeGraphStatus_NonAncestor(t *testing.T) {
	root, ggDir := setupIndexStatusRepo(t)
	first := gitCommit(t, root, "one.txt", "one")
	if err := state.Write(ggDir, first); err != nil {
		t.Fatalf("state.Write: %v", err)
	}

	git(t, root, "checkout", "--orphan", "other")
	_ = os.Remove(filepath.Join(root, "one.txt"))
	git(t, root, "rm", "-f", "one.txt")
	_ = gitCommit(t, root, "other.txt", "other")

	status := collectCodeGraphStatus(context.Background(), root, ggDir, nil)
	if status.Status != "non_ancestor" {
		t.Fatalf("status=%q detail=%q", status.Status, status.Detail)
	}
}

func TestCollectCodeGraphStatus_MissingAndPartial(t *testing.T) {
	root, ggDir := setupIndexStatusRepo(t)
	_ = gitCommit(t, root, "one.txt", "one")

	missing := collectCodeGraphStatus(context.Background(), root, ggDir, nil)
	if missing.Status != "missing" {
		t.Fatalf("missing status=%q detail=%q", missing.Status, missing.Detail)
	}

	if _, err := outbox.Write(ggDir, "changed-index", map[string]any{"root": root, "lang": "go", "sha": "abc"}); err != nil {
		t.Fatalf("outbox.Write: %v", err)
	}
	partial := collectCodeGraphStatus(context.Background(), root, ggDir, nil)
	if partial.Status != "partial" {
		t.Fatalf("partial status=%q detail=%q", partial.Status, partial.Detail)
	}
	if partial.PendingOutbox != 1 {
		t.Fatalf("PendingOutbox=%d want 1", partial.PendingOutbox)
	}
}

func TestRenderCodeGraphStatusCompact(t *testing.T) {
	out := renderCodeGraphStatusCompact(codeGraphStatus{
		Status:            "ready",
		LastIndexedSHA:    "1234567890abcdef",
		HeadSHA:           "1234567890abcdef",
		MemgraphAvailable: true,
	})
	for _, want := range []string{"CodeGraph ready", "idx=12345678", "head=12345678", "memgraph=ok"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in %q", want, out)
		}
	}
}

func setupIndexStatusRepo(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	ggDir := filepath.Join(root, ".gg")
	if err := os.MkdirAll(ggDir, 0o755); err != nil {
		t.Fatalf("mkdir .gg: %v", err)
	}
	git(t, root, "init")
	git(t, root, "config", "user.email", "test@example.com")
	git(t, root, "config", "user.name", "Test User")
	return root, ggDir
}

func gitCommit(t *testing.T, root, name, content string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, name), []byte(content+"\n"), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	git(t, root, "add", name)
	git(t, root, "commit", "-m", "commit "+name)
	out := git(t, root, "rev-parse", "HEAD")
	return strings.TrimSpace(out)
}

func git(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}
