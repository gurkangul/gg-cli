package cmd

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/index/changed"
	"github.com/gurkangul/gg-cli/internal/index/runner"
	"github.com/gurkangul/gg-cli/internal/index/state"
	"github.com/gurkangul/gg-cli/internal/outbox"
)

func TestCollectCodeGraphStatus_ReadyAndStale(t *testing.T) {
	root, ggDir := setupIndexStatusRepo(t)
	first := gitCommit(t, root, "one.go", "package main")
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

	if err := os.WriteFile(filepath.Join(root, "dirty.go"), []byte("package main\nvar dirty = true\n"), 0o644); err != nil {
		t.Fatalf("write dirty.go: %v", err)
	}
	dirty := collectCodeGraphStatus(context.Background(), root, ggDir, nil)
	if dirty.Status != "stale" || !strings.Contains(dirty.Detail, "1 new file") {
		t.Fatalf("dirty status=%q detail=%q", dirty.Status, dirty.Detail)
	}
	if dirty.NewFiles != 1 || dirty.ChangedFiles != 0 || dirty.DeletedFiles != 0 {
		t.Fatalf("dirty counts changed=%d new=%d deleted=%d", dirty.ChangedFiles, dirty.NewFiles, dirty.DeletedFiles)
	}
	git(t, root, "add", "dirty.go")
	git(t, root, "commit", "-m", "commit dirty.go")

	second := gitCommit(t, root, "two.go", "package main\nvar two = 2")
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
	first := gitCommit(t, root, "one.go", "package main")
	if err := state.Write(ggDir, first); err != nil {
		t.Fatalf("state.Write: %v", err)
	}

	git(t, root, "checkout", "--orphan", "other")
	_ = os.Remove(filepath.Join(root, "one.go"))
	git(t, root, "rm", "-f", "one.go")
	_ = gitCommit(t, root, "other.go", "package main")

	status := collectCodeGraphStatus(context.Background(), root, ggDir, nil)
	if status.Status != "non_ancestor" {
		t.Fatalf("status=%q detail=%q", status.Status, status.Detail)
	}
}

func TestCollectCodeGraphStatus_LanguageFingerprintDoesNotHideOtherDirtySources(t *testing.T) {
	root, ggDir := setupIndexStatusRepo(t)
	first := gitCommit(t, root, "one.go", "package main")
	if err := os.WriteFile(filepath.Join(root, "dirty.go"), []byte("package main\nvar dirty = true\n"), 0o644); err != nil {
		t.Fatalf("write dirty.go: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "dirty.ts"), []byte("export const dirty = true\n"), 0o644); err != nil {
		t.Fatalf("write dirty.ts: %v", err)
	}

	goFingerprint, err := changed.WorkingTreeFingerprint(context.Background(), root, first, []string{".go"})
	if err != nil {
		t.Fatalf("WorkingTreeFingerprint: %v", err)
	}
	if err := state.WriteWithFingerprint(ggDir, first, goFingerprint); err != nil {
		t.Fatalf("state.WriteWithFingerprint: %v", err)
	}

	status := collectCodeGraphStatus(context.Background(), root, ggDir, nil)
	if status.Status != "stale" || !strings.Contains(status.Detail, "2 new files") {
		t.Fatalf("status=%q detail=%q", status.Status, status.Detail)
	}
}

func TestCollectCodeGraphStatus_CountsChangedNewDeletedAndModuleFiles(t *testing.T) {
	root, ggDir := setupIndexStatusRepo(t)
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/app\ngo 1.22\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "delete_me.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write delete_me.go: %v", err)
	}
	git(t, root, "add", ".")
	git(t, root, "commit", "-m", "initial go module")
	first := strings.TrimSpace(git(t, root, "rev-parse", "HEAD"))
	if err := state.WriteLanguage(ggDir, "go", first, "", []string{".go"}); err != nil {
		t.Fatalf("state.WriteLanguage: %v", err)
	}

	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\nfunc main() { println(1) }\n"), 0o644); err != nil {
		t.Fatalf("modify main.go: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "new.go"), []byte("package main\nvar fresh = true\n"), 0o644); err != nil {
		t.Fatalf("write new.go: %v", err)
	}
	if err := os.Remove(filepath.Join(root, "delete_me.go")); err != nil {
		t.Fatalf("remove delete_me.go: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/app\ngo 1.23\n"), 0o644); err != nil {
		t.Fatalf("modify go.mod: %v", err)
	}

	status := collectCodeGraphStatus(context.Background(), root, ggDir, nil)
	if status.Status != "stale" {
		t.Fatalf("status=%q detail=%q", status.Status, status.Detail)
	}
	if status.ChangedFiles != 1 || status.NewFiles != 1 || status.DeletedFiles != 1 || status.ModuleFiles != 1 {
		t.Fatalf("counts changed=%d new=%d deleted=%d modules=%d detail=%q", status.ChangedFiles, status.NewFiles, status.DeletedFiles, status.ModuleFiles, status.Detail)
	}
	for _, want := range []string{"1 changed file", "1 new file", "1 deleted file", "1 module file changed", "gg index --lang go --changed"} {
		if !strings.Contains(status.Detail, want) {
			t.Fatalf("detail missing %q: %q", want, status.Detail)
		}
	}
}

func TestCollectCodeGraphStatus_DirtyIndexedTreeIsReadyWhenFingerprintMatches(t *testing.T) {
	root, ggDir := setupIndexStatusRepo(t)
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/app\ngo 1.22\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}
	git(t, root, "add", ".")
	git(t, root, "commit", "-m", "initial go module")
	head := strings.TrimSpace(git(t, root, "rev-parse", "HEAD"))
	if err := os.WriteFile(filepath.Join(root, "new.go"), []byte("package main\nvar fresh = true\n"), 0o644); err != nil {
		t.Fatalf("write new.go: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/app\ngo 1.23\n"), 0o644); err != nil {
		t.Fatalf("modify go.mod: %v", err)
	}
	fingerprint, err := changed.WorkingTreeFingerprintWithNames(context.Background(), root, head, []string{".go"}, []string{"go.mod"})
	if err != nil {
		t.Fatalf("WorkingTreeFingerprintWithNames: %v", err)
	}
	if err := state.WriteLanguage(ggDir, "go", head, fingerprint, []string{".go"}); err != nil {
		t.Fatalf("state.WriteLanguage: %v", err)
	}

	status := collectCodeGraphStatus(context.Background(), root, ggDir, nil)
	if status.Status != "ready" {
		t.Fatalf("status=%q detail=%q", status.Status, status.Detail)
	}
	if status.ChangedFiles+status.NewFiles+status.DeletedFiles+status.ModuleFiles != 0 {
		t.Fatalf("ready dirty-indexed status should not report stale counts: %#v", status)
	}
}

func TestCollectCodeGraphStatus_CommittedIndexedDirtyTreeIsReady(t *testing.T) {
	root, ggDir := setupIndexStatusRepo(t)
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/app\ngo 1.22\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}
	git(t, root, "add", ".")
	git(t, root, "commit", "-m", "initial go module")
	base := strings.TrimSpace(git(t, root, "rev-parse", "HEAD"))
	if err := os.WriteFile(filepath.Join(root, "new.go"), []byte("package main\nvar fresh = true\n"), 0o644); err != nil {
		t.Fatalf("write new.go: %v", err)
	}
	fingerprint, err := changed.WorkingTreeFingerprintWithNames(context.Background(), root, base, []string{".go"}, []string{"go.mod"})
	if err != nil {
		t.Fatalf("WorkingTreeFingerprintWithNames: %v", err)
	}
	if err := state.WriteLanguage(ggDir, "go", base, fingerprint, []string{".go"}); err != nil {
		t.Fatalf("state.WriteLanguage: %v", err)
	}
	git(t, root, "add", ".")
	git(t, root, "commit", "-m", "commit previously indexed dirty tree")

	status := collectCodeGraphStatus(context.Background(), root, ggDir, nil)
	if status.Status != "ready" {
		t.Fatalf("status=%q detail=%q last=%s head=%s", status.Status, status.Detail, status.LastIndexedSHA, status.HeadSHA)
	}
}

func TestCollectCodeGraphStatus_MissingAndPartial(t *testing.T) {
	root, ggDir := setupIndexStatusRepo(t)
	_ = gitCommit(t, root, "one.go", "package main")

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

func TestCollectCodeGraphStatus_IgnoresNonIndexOutbox(t *testing.T) {
	root, ggDir := setupIndexStatusRepo(t)
	first := gitCommit(t, root, "one.go", "package main")
	if err := state.Write(ggDir, first); err != nil {
		t.Fatalf("state.Write: %v", err)
	}
	if _, err := outbox.Write(ggDir, "task-replay", map[string]any{"uuid": "task-1"}); err != nil {
		t.Fatalf("outbox.Write: %v", err)
	}

	status := collectCodeGraphStatus(context.Background(), root, ggDir, nil)
	if status.Status != "ready" {
		t.Fatalf("status=%q detail=%q pending=%d", status.Status, status.Detail, status.PendingOutbox)
	}
	if status.PendingOutbox != 0 {
		t.Fatalf("PendingOutbox=%d want 0", status.PendingOutbox)
	}
}

func TestCollectCodeGraphStatus_MissingAfterEmptyInitWithGoCode(t *testing.T) {
	root, ggDir := setupIndexStatusRepo(t)
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/app\ngo 1.22\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "cmd", "app"), 0o755); err != nil {
		t.Fatalf("mkdir cmd/app: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "cmd", "app", "main.go"), []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}

	status := collectCodeGraphStatus(context.Background(), root, ggDir, nil)
	if status.Status != "missing" {
		t.Fatalf("status=%q detail=%q", status.Status, status.Detail)
	}
	if got := strings.Join(status.DetectedLanguages, ","); got != "go" {
		t.Fatalf("DetectedLanguages=%q", got)
	}
	for _, want := range []string{"project gained code since init", "gg index --lang go --changed"} {
		if !strings.Contains(status.Detail, want) {
			t.Fatalf("detail missing %q: %q", want, status.Detail)
		}
	}
	if status.SuggestedCommand != "gg index --lang go --changed" {
		t.Fatalf("SuggestedCommand=%q", status.SuggestedCommand)
	}
}

func TestCollectCodeGraphStatus_MissingAfterEmptyInitWithTypeScriptCode(t *testing.T) {
	root, ggDir := setupIndexStatusRepo(t)
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte("{\"name\":\"example\"}\n"), 0o644); err != nil {
		t.Fatalf("write package.json: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatalf("mkdir src: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "index.ts"), []byte("export const value = 1\n"), 0o644); err != nil {
		t.Fatalf("write index.ts: %v", err)
	}

	status := collectCodeGraphStatus(context.Background(), root, ggDir, nil)
	if status.Status != "missing" {
		t.Fatalf("status=%q detail=%q", status.Status, status.Detail)
	}
	if got := strings.Join(status.DetectedLanguages, ","); got != "typescript" {
		t.Fatalf("DetectedLanguages=%q", got)
	}
	if !strings.Contains(status.Detail, "project gained code since init") {
		t.Fatalf("detail missing init warning: %q", status.Detail)
	}
	if !strings.Contains(status.Detail, "gg index --lang typescript --changed") {
		t.Fatalf("detail missing suggested command: %q", status.Detail)
	}
	if status.SuggestedCommand != "gg index --lang typescript --changed" {
		t.Fatalf("SuggestedCommand=%q", status.SuggestedCommand)
	}
}

func TestCollectCodeGraphStatus_MissingLanguageStateHasStandardReason(t *testing.T) {
	root, ggDir := setupIndexStatusRepo(t)
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/app\ngo 1.22\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	sha := gitCommit(t, root, "main.go", "package main")
	if err := state.WriteLanguage(ggDir, "typescript", sha, "", []string{".ts", ".tsx"}); err != nil {
		t.Fatalf("state.WriteLanguage: %v", err)
	}

	status := collectCodeGraphStatus(context.Background(), root, ggDir, nil)
	if status.Status != "stale" {
		t.Fatalf("status=%q detail=%q", status.Status, status.Detail)
	}
	if status.CodeGraph.Reason != codeGraphReasonLanguageMissing {
		t.Fatalf("codegraph=%+v detail=%q", status.CodeGraph, status.Detail)
	}
	if status.CodeGraph.SuggestedCommand != codeGraphRepairCommand {
		t.Fatalf("SuggestedCommand=%q", status.CodeGraph.SuggestedCommand)
	}
}

func TestCollectCodeGraphStatus_SourceWithoutIndexableModuleDoesNotSuggestImpossibleIndex(t *testing.T) {
	root, ggDir := setupIndexStatusRepo(t)
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}

	status := collectCodeGraphStatus(context.Background(), root, ggDir, nil)
	if status.Status != "missing" {
		t.Fatalf("status=%q detail=%q", status.Status, status.Detail)
	}
	if len(status.DetectedLanguages) != 0 {
		t.Fatalf("DetectedLanguages=%v", status.DetectedLanguages)
	}
	if status.SuggestedCommand != "" {
		t.Fatalf("SuggestedCommand=%q", status.SuggestedCommand)
	}
	for _, want := range []string{"project gained supported source files", "no indexable module", "go.mod"} {
		if !strings.Contains(status.Detail, want) {
			t.Fatalf("detail missing %q: %q", want, status.Detail)
		}
	}
}

func TestRenderCodeGraphStatusCompact(t *testing.T) {
	out := renderCodeGraphStatusCompact(codeGraphStatus{
		Status:            "ready",
		LastIndexedSHA:    "1234567890abcdef",
		HeadSHA:           "1234567890abcdef",
		MemgraphAvailable: true,
	})
	for _, want := range []string{"CodeGraph ready", "idx=12345678", "head=12345678", "codegraph=ok"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in %q", want, out)
		}
	}
}

func TestCodeGraphStatus_FinalizeDowngradesReadyWhenMemgraphUnavailable(t *testing.T) {
	status := codeGraphStatus{
		Status:            "ready",
		Detail:            "index-state matches HEAD and working tree source files for go",
		DetectedLanguages: []string{"go"},
		SuggestedCommand:  "gg index --lang go --changed",
		MemgraphDetail:    "unavailable: dial tcp 127.0.0.1:1: connect: connection refused",
	}
	status.finalize()
	if status.Status != "missing" {
		t.Fatalf("Status=%q Detail=%q", status.Status, status.Detail)
	}
	for _, want := range []string{"code graph unavailable", "check the code graph (.gg/graph.db)", "gg index --lang go"} {
		if !strings.Contains(status.Detail, want) {
			t.Fatalf("detail missing %q: %q", want, status.Detail)
		}
	}
	if strings.Contains(status.Detail, "--changed") || strings.Contains(status.SuggestedCommand, "--changed") {
		t.Fatalf("unavailable projection should prefer full index, detail=%q suggested=%q", status.Detail, status.SuggestedCommand)
	}
}

func TestCodeGraphStatus_FinalizeDowngradesReadyWhenGraphStatsUnavailable(t *testing.T) {
	status := codeGraphStatus{
		Status:              "ready",
		Detail:              "index-state matches HEAD and working tree source files for go",
		MemgraphAvailable:   true,
		MemgraphDetail:      "stats unavailable: query failed",
		DetectedLanguages:   []string{"go"},
		SuggestedCommand:    "gg index --lang go --changed",
		GraphStatsAvailable: false,
	}
	status.finalize()
	if status.Status != "missing" {
		t.Fatalf("Status=%q Detail=%q", status.Status, status.Detail)
	}
	for _, want := range []string{"code graph stats unavailable", "run gg doctor", "gg index --lang go"} {
		if !strings.Contains(status.Detail, want) {
			t.Fatalf("detail missing %q: %q", want, status.Detail)
		}
	}
	if strings.Contains(status.Detail, "--changed") || strings.Contains(status.SuggestedCommand, "--changed") {
		t.Fatalf("stats-unavailable projection should prefer full index, detail=%q suggested=%q", status.Detail, status.SuggestedCommand)
	}
}

func TestRenderCodeGraphStatusCompact_IncludesDetectedAndRunCommand(t *testing.T) {
	out := renderCodeGraphStatusCompact(codeGraphStatus{
		Status:            "missing",
		DetectedLanguages: []string{"typescript"},
		SuggestedCommand:  "gg index --lang typescript --changed",
	})
	for _, want := range []string{"CodeGraph missing", "reason=missing_graph", "detected=typescript", "run=gg doctor --fix-index", "watch=gg index --watch --lang typescript"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in %q", want, out)
		}
	}
}

func TestEmitCodeGraphNotice_SurfacesWarningAndRecommendation(t *testing.T) {
	setupGGDir(t)
	git(t, ".", "init")
	git(t, ".", "config", "user.email", "test@example.com")
	git(t, ".", "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(".", "go.mod"), []byte("module example.com/app\ngo 1.22\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(".", "cmd", "app"), 0o755); err != nil {
		t.Fatalf("mkdir cmd/app: %v", err)
	}
	if err := os.WriteFile(filepath.Join(".", "cmd", "app", "main.go"), []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}

	loadedCfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	var buf bytes.Buffer
	emitCodeGraphNotice(context.Background(), &buf, loadedCfg)
	out := buf.String()
	for _, want := range []string{"CODE GRAPH NOTICE", "CodeGraph:", "gg doctor --fix-index", "gg index --watch --lang go", "does not run a background index daemon"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

func TestCodeGraphFullIndexSuggestion_UsesFullIndex(t *testing.T) {
	got := codeGraphFullIndexSuggestion([]runner.Lang{runner.LangGo})
	if got != "gg index --lang go" {
		t.Fatalf("got %q, want %q", got, "gg index --lang go")
	}
}

func TestCollectCodeGraphStatus_GraphEmptySuggestsFullIndex(t *testing.T) {
	root, ggDir := setupIndexStatusRepo(t)
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/app\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}
	sha := gitCommit(t, root, "main.go", "package main")
	if err := state.Write(ggDir, sha); err != nil {
		t.Fatalf("state.Write: %v", err)
	}
	status := collectCodeGraphStatus(context.Background(), root, ggDir, nil)
	status.GraphEmpty = true
	status.MemgraphAvailable = true
	status.Status = "missing"
	status.finalize()
	if !strings.Contains(status.Detail, "gg index --lang go") || strings.Contains(status.Detail, "--changed") {
		t.Fatalf("detail should prefer full index, got %q", status.Detail)
	}
	if status.SuggestedCommand != "gg index --lang go" {
		t.Fatalf("SuggestedCommand=%q, want full index", status.SuggestedCommand)
	}
}

func TestEmitCodeGraphNotice_NoFixIndexWhenNoIndexableModule(t *testing.T) {
	setupGGDir(t)
	git(t, ".", "init")
	git(t, ".", "config", "user.email", "test@example.com")
	git(t, ".", "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(".", "main.go"), []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}

	loadedCfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	var buf bytes.Buffer
	emitCodeGraphNotice(context.Background(), &buf, loadedCfg)
	out := buf.String()
	for _, want := range []string{"CODE GRAPH NOTICE", "no indexable module", "add a supported module manifest", "does not run a background index daemon"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "gg doctor --fix-index") {
		t.Fatalf("unexpected fix-index suggestion in non-indexable repo:\n%s", out)
	}
}

func TestCodeGraphFreshnessContract_ReadyAndMissing(t *testing.T) {
	ready := codeGraphStatus{Status: "ready", Detail: "index-state matches HEAD and indexed dirty working tree source fingerprint for go", DetectedLanguages: []string{"go"}}
	fresh := ready.freshnessContract()
	if fresh.Status != codeGraphFreshnessReady || fresh.BackgroundRefresh {
		t.Fatalf("ready freshness=%+v", fresh)
	}
	if fresh.Reason != "" {
		t.Fatalf("ready reason=%q, want empty", fresh.Reason)
	}
	if fresh.SuggestedCommand != "" || !fresh.ForegroundWatchAvailable || fresh.ForegroundWatchCommand != "gg index --watch --lang go" {
		t.Fatalf("ready command fields=%+v", fresh)
	}

	missing := codeGraphStatus{Status: "missing", DetectedLanguages: []string{"go"}, SuggestedCommand: "gg index --lang go --changed"}
	fresh = missing.freshnessContract()
	if fresh.Status != codeGraphFreshnessMissing || fresh.Reason != codeGraphReasonMissingGraph {
		t.Fatalf("missing freshness=%+v", fresh)
	}
	if fresh.SuggestedCommand != codeGraphRepairCommand || fresh.BackgroundRefresh {
		t.Fatalf("missing command/background=%+v", fresh)
	}
}

func TestCodeGraphFreshnessContract_Reasons(t *testing.T) {
	cases := []struct {
		name   string
		status codeGraphStatus
		wantS  string
		wantR  string
	}{
		{"empty graph", codeGraphStatus{Status: "missing", GraphEmpty: true, DetectedLanguages: []string{"go"}}, codeGraphFreshnessMissing, codeGraphReasonEmptyGraph},
		{"changed files", codeGraphStatus{Status: "stale", ChangedFiles: 1, DetectedLanguages: []string{"go"}}, codeGraphFreshnessStale, codeGraphReasonChangedFiles},
		{"module manifest", codeGraphStatus{Status: "stale", ModuleFiles: 1, DetectedLanguages: []string{"go"}}, codeGraphFreshnessStale, codeGraphReasonModuleManifestChanged},
		{"language missing", codeGraphStatus{Status: "stale", Detail: "unindexed language(s): go", DetectedLanguages: []string{"go"}}, codeGraphFreshnessStale, codeGraphReasonLanguageMissing},
		{"non ancestor", codeGraphStatus{Status: "non_ancestor", DetectedLanguages: []string{"go"}}, codeGraphFreshnessStale, codeGraphReasonNonAncestor},
		{"memgraph unavailable", codeGraphStatus{Status: "ready", MemgraphConfigured: true, MemgraphDetail: "unavailable: dial tcp", DetectedLanguages: []string{"go"}}, codeGraphFreshnessUnavailable, codeGraphReasonMemgraphUnavailable},
		{"not applicable", codeGraphStatus{Status: "not_applicable"}, codeGraphFreshnessNotApplicable, codeGraphReasonNotApplicable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fresh := tc.status.freshnessContract()
			if fresh.Status != tc.wantS || fresh.Reason != tc.wantR {
				t.Fatalf("freshness=%+v want status=%s reason=%s", fresh, tc.wantS, tc.wantR)
			}
			if fresh.BackgroundRefresh {
				t.Fatalf("background refresh must be false: %+v", fresh)
			}
		})
	}
}

func TestCodeGraphFreshnessContract_HumanNotice(t *testing.T) {
	status := codeGraphStatus{Status: "stale", ChangedFiles: 1, DetectedLanguages: []string{"go"}, SuggestedCommand: "gg index --lang go --changed"}
	notice := codeGraphNoticeOneLine(status)
	for _, want := range []string{"CodeGraph: stale (changed_files)", "gg doctor --fix-index", "gg index --watch --lang go", "No background index daemon"} {
		if !strings.Contains(notice, want) {
			t.Fatalf("missing %q in %q", want, notice)
		}
	}
}

func TestCodeGraphFreshnessContract_JSONShape(t *testing.T) {
	status := codeGraphStatus{Status: "stale", ChangedFiles: 1, DetectedLanguages: []string{"go"}, SuggestedCommand: "gg index --lang go --changed"}
	status.finalize()
	if status.CodeGraph.Status != codeGraphFreshnessStale || status.CodeGraph.Reason != codeGraphReasonChangedFiles {
		t.Fatalf("nested codegraph=%+v", status.CodeGraph)
	}
	if status.CodeGraph.SuggestedCommand != codeGraphRepairCommand || status.CodeGraph.ForegroundWatchCommand != "gg index --watch --lang go" {
		t.Fatalf("nested command fields=%+v", status.CodeGraph)
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
