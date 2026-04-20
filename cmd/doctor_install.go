package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/agenthooks"
	"github.com/gurkangul/gg-cli/internal/artifacts"
	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/templates"
)

// runDoctorInstallAgentHooks is the subcommand dispatched by
// `gg doctor --install-agent-hooks`. It runs the agenthooks package against
// the project root and prints a report. --agent restricts the run to a
// single installer; without it, auto-detect installs all present agents.
func runDoctorInstallAgentHooks(_ *cobra.Command) error {
	root, err := config.FindRoot()
	if err != nil {
		return configErr("run `gg init` first: " + err.Error())
	}

	opts := agenthooks.Options{DryRun: doctorHooksDryRun, Force: doctorHooksForce}

	var results []agenthooks.Result
	switch {
	case strings.TrimSpace(doctorHooksAgent) != "":
		names := splitCSV(doctorHooksAgent)
		results = agenthooks.InstallNamed(root, names, opts)
	default:
		results = agenthooks.InstallDetected(root, opts)
	}

	fmt.Println("Agent Hooks")
	fmt.Println(strings.Repeat("─", 50))
	agenthooks.RenderReport(os.Stdout, results)
	fmt.Println(strings.Repeat("─", 50))
	fmt.Println(agenthooks.Summary(results))

	if agenthooks.HasProblems(results) {
		return fmt.Errorf("one or more installers failed — see report above")
	}
	return nil
}

// splitCSV parses a comma-separated flag value into a cleaned slice.
// Empty tokens are dropped so --agent="claude,," still yields ["claude"].
func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// hookInstallSettings resolves the installer's walk depth and skip-directory
// list. Precedence:
//
//  1. Values explicitly set in .gg/config.yaml under doctor.hook_install.
//  2. Built-in defaults (config.DefaultHookInstallSkipDirs /
//     config.DefaultHookInstallMaxDepth).
//
// Returning a lookup map keeps the per-directory prune check O(1).
func hookInstallSettings() (skipDirs map[string]bool, maxDepth int) {
	maxDepth = config.DefaultHookInstallMaxDepth
	dirs := config.DefaultHookInstallSkipDirs

	if cfg, err := config.Load(); err == nil {
		if cfg.Doctor.HookInstall.MaxDepth > 0 {
			maxDepth = cfg.Doctor.HookInstall.MaxDepth
		}
		if len(cfg.Doctor.HookInstall.SkipDirs) > 0 {
			dirs = cfg.Doctor.HookInstall.SkipDirs
		}
	}

	skipDirs = make(map[string]bool, len(dirs))
	for _, d := range dirs {
		skipDirs[d] = true
	}
	return skipDirs, maxDepth
}

// runDoctorInstallTaskHooks installs the verify-gate pre-task-done hooks and
// the advisory post-done hook(s) for every language/manifest it finds inside
// the project — including nested monorepo packages. Existing files are
// preserved so repeated runs are idempotent.
//
// Detection (relative paths up to config.DefaultHookInstallMaxDepth, overridable
// via doctor.hook_install.max_depth in .gg/config.yaml):
//   - go.mod         → Go pre-hook (blocking: build + vet + test) + legacy post hook
//   - package.json   → Node/Bun/pnpm/Yarn pre-hook (install + typecheck + build + test)
//
// Scripts are named per detected location so a monorepo with `api/go.mod` and
// `cli/go.mod` produces two separate gate scripts that each cd into their
// own directory before running the checks.
//
// Nothing matches → print a copy-pasteable manual snippet and return without
// error so the caller's exit code stays 0 (no-op is not a failure).
func runDoctorInstallTaskHooks() error {
	ggDir, err := config.GGDir()
	if err != nil {
		return err
	}
	projectRoot, err := config.FindRoot()
	if err != nil {
		return err
	}

	preDir := filepath.Join(ggDir, "hooks", "pre-task-done.d")
	postDir := filepath.Join(ggDir, "hooks", "task-done.d")
	if mkErr := os.MkdirAll(preDir, 0o755); mkErr != nil {
		return fmt.Errorf("create pre-hook dir: %w", mkErr)
	}
	if mkErr := os.MkdirAll(postDir, 0o755); mkErr != nil {
		return fmt.Errorf("create post-hook dir: %w", mkErr)
	}

	skipDirs, maxDepth := hookInstallSettings()
	goDirs, err := findManifestDirs(projectRoot, "go.mod", skipDirs, maxDepth)
	if err != nil {
		return fmt.Errorf("walk for go.mod: %w", err)
	}
	nodeDirs, err := findManifestDirs(projectRoot, "package.json", skipDirs, maxDepth)
	if err != nil {
		return fmt.Errorf("walk for package.json: %w", err)
	}

	installed := 0

	for _, sub := range goDirs {
		prePath := filepath.Join(preDir, hookFileName("10-go-verify", sub))
		postPath := filepath.Join(postDir, hookFileName("10-go-quality", sub))

		preBody := strings.ReplaceAll(templates.PreTaskDoneGoHook, "__GG_SUBDIR__", sub)
		// Advisory post-hook is the legacy inline template — rewrite its shell
		// block with the cd prefix if it's running in a subdirectory. For now
		// the template assumes root; wrap it inside a cd subshell when needed.
		postBody := wrapLegacyPostHook(templates.TaskDoneGoHook, sub)

		if n, err := installHookIfAbsent(prePath, preBody,
			fmt.Sprintf("Go verify gate at %s — blocking (build + vet + test)", sub)); err != nil {
			return err
		} else {
			installed += n
		}
		if n, err := installHookIfAbsent(postPath, postBody,
			fmt.Sprintf("Go post-done at %s — advisory (vet + test + lint)", sub)); err != nil {
			return err
		} else {
			installed += n
		}
	}

	for _, sub := range nodeDirs {
		prePath := filepath.Join(preDir, hookFileName("10-node-verify", sub))
		preBody := strings.ReplaceAll(templates.PreTaskDoneNodeHook, "__GG_SUBDIR__", sub)
		if n, err := installHookIfAbsent(prePath, preBody,
			fmt.Sprintf("Node verify gate at %s — blocking (install + typecheck + build + test)", sub)); err != nil {
			return err
		} else {
			installed += n
		}
	}

	// Regression gate: always install 90-bug-repros.sh regardless of language.
	bugReprosPath := filepath.Join(preDir, "90-bug-repros.sh")
	if n, err := installHookIfAbsent(bugReprosPath, templates.BugReprosHook,
		"regression gate — runs all repro scripts for fixed bugs (GG_ENFORCEMENT controls blocking)"); err != nil {
		return err
	} else {
		installed += n
	}

	// Test-tier smoke gate: install everywhere, self-skips until project adopts test-tier Makefile pattern.
	smokeHookPath := filepath.Join(preDir, "05-smoke-e2e.sh")
	if n, err := installHookIfAbsent(smokeHookPath, templates.SmokeE2EHook,
		"smoke gate — runs `make test-smoke` when the target exists (skips quietly otherwise)"); err != nil {
		return err
	} else {
		installed += n
	}

	// Decision-capture gate: warns (or blocks with GG_DECIDE_GATE=block)
	// when `gg task done` runs on a task with zero decisions linked.
	decideHookPath := filepath.Join(preDir, "20-decide-capture.sh")
	if n, err := installHookIfAbsent(decideHookPath, templates.PreTaskDoneDecideCaptureHook,
		"decide gate — warns on task close without a linked gg record (GG_DECIDE_GATE=warn|block|off)"); err != nil {
		return err
	} else {
		installed += n
	}

	// 30-file-size.sh: warns (or blocks) when source/test files exceed the
	// 500/800-line modularity cap.
	fileSizeHookPath := filepath.Join(preDir, "30-file-size.sh")
	if n, err := installHookIfAbsent(fileSizeHookPath, templates.FileSizeGateHook,
		"file-size gate — warns on oversized source/test files (GG_FILE_SIZE_GATE=warn|block|off)"); err != nil {
		return err
	} else {
		installed += n
	}

	// Makefile tier template: written to .gg/templates/ so humans can discover
	// + opt-in via `include .gg/templates/makefile-test-tiers.mk`.
	tmplDir := filepath.Join(ggDir, "templates")
	if mkErr := os.MkdirAll(tmplDir, 0o755); mkErr != nil {
		return fmt.Errorf("create templates dir: %w", mkErr)
	}
	mkTierPath := filepath.Join(tmplDir, "makefile-test-tiers.mk")
	if _, statErr := os.Stat(mkTierPath); os.IsNotExist(statErr) {
		if writeErr := os.WriteFile(mkTierPath, []byte(templates.MakefileTestTiers), 0o644); writeErr != nil {
			return fmt.Errorf("write %s: %w", mkTierPath, writeErr)
		}
		fmt.Printf("  ✓ template .gg/templates/makefile-test-tiers.mk written — add `include` line to adopt\n")
		installed++
	}

	// Offer to add the include line to an existing Makefile.
	makefilePath := filepath.Join(projectRoot, "Makefile")
	if _, statErr := os.Stat(makefilePath); statErr == nil {
		if err := offerMakefileTestTierInclude(makefilePath, mkTierPath, projectRoot); err != nil {
			fmt.Fprintf(os.Stderr, "⚠ Makefile update skipped: %v\n", err)
		}
	}

	if len(goDirs) == 0 && len(nodeDirs) == 0 {
		fmt.Println("No go.mod or package.json found in the project (walked up to depth", maxDepth, ").")
		fmt.Println("Write your own verify gate at:")
		fmt.Printf("  %s/10-custom-verify.sh\n", preDir)
		fmt.Println("Env vars your script receives: GG_TASK_ID, GG_TASK_SUMMARY, GG_PROJECT_ID, GG_ACTOR.")
		fmt.Println("Non-zero exit blocks `gg task done` with exit code 7.")
		return nil
	}

	// Record installed artifacts in .gg/installed.json so `gg doctor --sync-artifacts`
	// can detect drift when the CLI is upgraded.
	recordTaskHookArtifacts(ggDir)

	if installed == 0 {
		fmt.Println("All detected hooks already present — nothing to do (remove files manually to reinstall).")
		return nil
	}

	fmt.Println()
	fmt.Println("Verify gate is live. Next `gg task done` run will:")
	fmt.Printf("  1. execute %s/*.sh in order (any non-zero exit → block, exit 7)\n", preDir)
	fmt.Printf("  2. write the new state to the store on success\n")
	fmt.Printf("  3. run %s/*.sh as advisory (warnings only unless hooks.strict=true)\n", postDir)
	fmt.Println()
	fmt.Println("Edit the scripts freely — they ship as starting points, not contracts.")
	return nil
}

// recordTaskHookArtifacts stamps .gg/installed.json with the current CLI
// template SHAs for all task-hook artifacts. Called after install so that
// `gg doctor --sync-artifacts` knows what version was installed.
// Errors are silently swallowed — manifest drift is advisory, never fatal.
func recordTaskHookArtifacts(ggDir string) {
	current := templates.ArtifactSHAs()
	m, err := artifacts.Read(ggDir)
	if err != nil {
		return
	}
	for k, sha := range current {
		m.Artifacts[k] = sha
	}
	_ = artifacts.Write(ggDir, m)
}

// findManifestDirs walks root up to maxDepth and returns the list of
// directories (relative to root, using '/' as separator) that contain a file
// named manifestName. Slash-delimited "." represents the root. Directories
// whose base name is a key in skipDirs are pruned from the walk.
func findManifestDirs(root, manifestName string, skipDirs map[string]bool, maxDepth int) ([]string, error) {
	var out []string
	rootClean := filepath.Clean(root)

	err := filepath.WalkDir(rootClean, func(path string, de os.DirEntry, err error) error {
		if err != nil {
			if de != nil && de.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if de.IsDir() {
			if path == rootClean {
				return nil
			}
			rel, relErr := filepath.Rel(rootClean, path)
			if relErr != nil {
				return nil
			}
			if skipDirs[de.Name()] {
				return filepath.SkipDir
			}
			depth := len(strings.Split(filepath.ToSlash(rel), "/"))
			if depth > maxDepth {
				return filepath.SkipDir
			}
			return nil
		}
		if de.Name() != manifestName {
			return nil
		}
		rel, relErr := filepath.Rel(rootClean, filepath.Dir(path))
		if relErr != nil {
			return nil
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// hookFileName builds the target script filename for a given relative dir.
// Root (".") keeps the bare base name so existing installs don't churn;
// nested paths get a slug suffix so two Go modules land in distinct files.
func hookFileName(base, sub string) string {
	if sub == "." || sub == "" {
		return base + ".sh"
	}
	slug := strings.ReplaceAll(sub, "/", "-")
	return base + "-" + slug + ".sh"
}

// wrapLegacyPostHook returns the post-hook body adjusted so commands run from
// the manifest directory. The legacy template (task-done-go.sh) assumes root;
// if the manifest lives in a subdir we wrap the body with a cd line so the
// same template keeps working without churn.
func wrapLegacyPostHook(body, sub string) string {
	if sub == "." || sub == "" {
		return body
	}
	header := "#!/bin/sh\n# Auto-wrapped by gg installer — original body runs inside " + sub + "\nset -e\ncd \"$(dirname \"$0\")/../../..\"\ncd \"" + sub + "\"\n\n"
	// Drop the original shebang and any leading blank so the wrapped script
	// has exactly one shebang line.
	trimmed := body
	if strings.HasPrefix(trimmed, "#!/") {
		if idx := strings.Index(trimmed, "\n"); idx >= 0 {
			trimmed = trimmed[idx+1:]
		}
	}
	return header + trimmed
}

// offerMakefileTestTierInclude checks whether makefilePath already includes the
// test-tier template. If not, it prompts the user interactively and appends the
// include line on confirmation.
func offerMakefileTestTierInclude(makefilePath, tierTemplatePath, projectRoot string) error {
	data, err := os.ReadFile(makefilePath) //nolint:gosec
	if err != nil {
		return fmt.Errorf("read Makefile: %w", err)
	}
	if strings.Contains(string(data), "makefile-test-tiers.mk") {
		fmt.Println("  ✓ Makefile already includes test-tier template — nothing to do")
		return nil
	}

	rel, _ := filepath.Rel(projectRoot, tierTemplatePath)
	includeLine := "include " + filepath.ToSlash(rel)

	fmt.Printf("\nMakefile found at %s.\n", makefilePath)
	fmt.Printf("Add test-tier targets (test-unit / test-integration / test-smoke / test-e2e)? [y/N] ")
	fmt.Printf("  This appends: %s\n", includeLine)

	var answer string
	if _, scanErr := fmt.Scanln(&answer); scanErr != nil || strings.ToLower(strings.TrimSpace(answer)) != "y" {
		fmt.Println("  Skipped — add the include manually when ready.")
		return nil
	}

	f, openErr := os.OpenFile(makefilePath, os.O_APPEND|os.O_WRONLY, 0o644) //nolint:gosec
	if openErr != nil {
		return fmt.Errorf("open Makefile: %w", openErr)
	}
	defer func() { _ = f.Close() }()
	if _, writeErr := fmt.Fprintf(f, "\n# gg test-tier targets\n%s\n", includeLine); writeErr != nil {
		return fmt.Errorf("append to Makefile: %w", writeErr)
	}
	fmt.Printf("  ✓ Added '%s' to Makefile\n", includeLine)
	return nil
}

// installHookIfAbsent writes body to path with 0755 permissions, unless a file
// already exists there. Returns 1 if the file was written, 0 if skipped.
func installHookIfAbsent(path, body, summary string) (int, error) {
	if _, err := os.Stat(path); err == nil {
		fmt.Printf("⚠ %s already exists — skipping (remove it manually to reinstall)\n", path)
		return 0, nil
	}
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		return 0, fmt.Errorf("write %s: %w", path, err)
	}
	fmt.Printf("✓ Installed %s\n", path)
	fmt.Printf("  %s\n", summary)
	return 1, nil
}

