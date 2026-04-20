package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gurkangul/gg-cli/internal/artifacts"
	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/filesize"
	"github.com/gurkangul/gg-cli/internal/templates"
)

// doctorCheckArtifactDrift reads installed.json and counts drifted/missing
// artifacts. Returns 0 when the manifest is missing (not yet initialized).
func doctorCheckArtifactDrift() int {
	root, err := config.FindRoot()
	if err != nil {
		return 0
	}
	ggDir := filepath.Join(root, ".gg")
	m, err := artifacts.Read(ggDir)
	if err != nil || len(m.Artifacts) == 0 {
		return 0
	}
	current := templates.ArtifactSHAs()
	entries := artifacts.Diff(m, current)
	n := 0
	for _, e := range entries {
		if e.Status != "ok" {
			n++
		}
	}
	return n
}

// runDoctorSyncArtifacts implements `gg doctor --sync-artifacts [--apply]`.
func runDoctorSyncArtifacts(apply bool) error {
	root, err := config.FindRoot()
	if err != nil {
		return fmt.Errorf("not inside a gg project: %w", err)
	}
	ggDir := filepath.Join(root, ".gg")
	m, err := artifacts.Read(ggDir)
	if err != nil {
		return fmt.Errorf("read installed.json: %w", err)
	}
	current := templates.ArtifactSHAs()
	entries := artifacts.Diff(m, current)

	fmt.Println("Artifact Drift Report")
	fmt.Println(strings.Repeat("─", 60))
	fmt.Printf("  %-46s  %s\n", "Artifact", "Status")
	fmt.Println(strings.Repeat("─", 60))

	drifted := 0
	for _, e := range entries {
		marker := "✓"
		switch e.Status {
		case "drifted":
			marker = "~"
			drifted++
		case "missing":
			marker = "?"
			drifted++
		}
		fmt.Printf("  %s %-44s  %s\n", marker, e.Key, e.Status)
	}
	fmt.Println(strings.Repeat("─", 60))

	if drifted == 0 {
		fmt.Println("All artifacts up to date.")
		return nil
	}
	fmt.Printf("%d artifact(s) need attention.\n", drifted)

	if !apply {
		fmt.Println("Run with --apply to re-install drifted artifacts.")
		return nil
	}

	applied := 0
	for _, e := range entries {
		if e.Status == "ok" {
			continue
		}
		if err := syncArtifactFile(ggDir, e.Key); err != nil {
			fmt.Fprintf(os.Stderr, "  ✗ %s: %v\n", e.Key, err)
			continue
		}
		if err := artifacts.Record(ggDir, e.Key, e.Current); err != nil {
			fmt.Fprintf(os.Stderr, "  ✗ record %s: %v\n", e.Key, err)
			continue
		}
		fmt.Printf("  ✓ restored %s\n", e.Key)
		applied++
	}
	fmt.Printf("%d artifact(s) restored.\n", applied)
	return nil
}

// syncArtifactFile writes the current CLI template content for key to
// <ggDir>/<key>, creating parent directories as needed.
func syncArtifactFile(ggDir, key string) error {
	content, ok := artifactContent(key)
	if !ok {
		return fmt.Errorf("unknown artifact key %q", key)
	}
	path := filepath.Join(ggDir, filepath.FromSlash(key))
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	perm := os.FileMode(0o644)
	if strings.HasSuffix(key, ".sh") {
		perm = 0o755
	}
	return os.WriteFile(path, []byte(content), perm) //nolint:gosec
}

// artifactContent returns the bundled template body for a given key.
func artifactContent(key string) (string, bool) {
	switch key {
	case "RULES.md":
		return templates.RulesMD, true
	case "AGENTS.md":
		return templates.AgentsMD, true
	case "hooks/pre-task-done.d/05-smoke-e2e.sh":
		return templates.SmokeE2EHook, true
	case "hooks/task-done.d/90-bug-repros.sh":
		return templates.BugReprosHook, true
	case "hooks/pre-task-done.d/10-go-verify.sh":
		return templates.PreTaskDoneGoHook, true
	case "hooks/pre-task-done.d/10-node-verify.sh":
		return templates.PreTaskDoneNodeHook, true
	case "hooks/pre-task-done.d/20-decide-capture.sh":
		return templates.PreTaskDoneDecideCaptureHook, true
	case "hooks/task-done.d/80-task-done-go.sh":
		return templates.TaskDoneGoHook, true
	case "templates/makefile-test-tiers.mk":
		return templates.MakefileTestTiers, true
	}
	return "", false
}

// runDoctorSyncBaseline rescans the project and rewrites .gg/file-size-baseline.json
// with current line counts for all files that currently exceed their size limit.
// Files already under the limit are omitted — the baseline only grandfathers violations.
func runDoctorSyncBaseline() error {
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	allFiles, err := filesize.ScanDir(root)
	if err != nil {
		return fmt.Errorf("scanning project: %w", err)
	}

	b := &filesize.Baseline{Files: map[string]int{}}
	for path, lines := range allFiles {
		limit := filesize.ParseLimit(path)
		if lines > limit {
			b.Files[path] = lines
		}
	}

	if err := filesize.WriteBaseline(root, b); err != nil {
		return fmt.Errorf("writing baseline: %w", err)
	}

	fmt.Printf("file-size baseline updated: %d file(s) grandfathered in .gg/file-size-baseline.json\n", len(b.Files))
	return nil
}
