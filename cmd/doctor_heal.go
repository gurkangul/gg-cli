package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/templates"
)

// runDoctorHeal migrates legacy runtime state from .gg/ to the per-project
// runtime directory (~/.gg/projects/<project_id>/). It is safe to run multiple
// times — each step is idempotent:
//
//   - .gg/telemetry.jsonl → appended to runtimeDir/telemetry.jsonl, then removed.
//   - .gg/cache/          → moved to runtimeDir/cache/, then removed.
//
// The function prints git rm --cached suggestions but never runs git itself.
func runDoctorHeal() error {
	cfg, err := config.Load()
	if err != nil {
		return configErr("run `gg init` first: " + err.Error())
	}
	ggDir, err := config.GGDir()
	if err != nil {
		return configErr("run `gg init` first: " + err.Error())
	}
	rtDir, err := cfg.RuntimeDir()
	if err != nil {
		return fmt.Errorf("create runtime dir: %w", err)
	}

	fmt.Println("GG Doctor — heal (runtime state migration)")
	fmt.Println(strings.Repeat("─", 50))

	migrated := false
	var gitRmTargets []string

	// ── Telemetry migration ──────────────────────────────────────────────────
	srcTelemetry := filepath.Join(ggDir, "telemetry.jsonl")
	if info, statErr := os.Stat(srcTelemetry); statErr == nil && !info.IsDir() {
		dstTelemetry := filepath.Join(rtDir, "telemetry.jsonl")
		if appendErr := appendFile(dstTelemetry, srcTelemetry); appendErr != nil {
			fmt.Printf("  ✗ telemetry.jsonl: append failed: %v\n", appendErr)
		} else if rmErr := os.Remove(srcTelemetry); rmErr != nil {
			fmt.Printf("  ~ telemetry.jsonl: appended but could not remove source: %v\n", rmErr)
		} else {
			fmt.Printf("  ✓ telemetry.jsonl → %s\n", dstTelemetry)
			gitRmTargets = append(gitRmTargets, ".gg/telemetry.jsonl")
			migrated = true
		}
	} else {
		fmt.Println("  ✓ telemetry.jsonl  (not in .gg/ — nothing to migrate)")
	}

	// ── Cache migration ──────────────────────────────────────────────────────
	srcCache := filepath.Join(ggDir, "cache")
	if info, statErr := os.Stat(srcCache); statErr == nil && info.IsDir() {
		dstCache := filepath.Join(rtDir, "cache")
		if renameErr := os.Rename(srcCache, dstCache); renameErr != nil {
			// Cross-device rename can fail — fall back to copy-then-remove.
			if copyErr := copyDir(srcCache, dstCache); copyErr != nil {
				fmt.Printf("  ✗ cache/: migration failed: %v\n", copyErr)
			} else if rmErr := os.RemoveAll(srcCache); rmErr != nil {
				fmt.Printf("  ~ cache/: copied but could not remove source: %v\n", rmErr)
			} else {
				fmt.Printf("  ✓ cache/ → %s\n", dstCache)
				gitRmTargets = append(gitRmTargets, ".gg/cache/")
				migrated = true
			}
		} else {
			fmt.Printf("  ✓ cache/ → %s\n", dstCache)
			gitRmTargets = append(gitRmTargets, ".gg/cache/")
			migrated = true
		}
	} else {
		fmt.Println("  ✓ cache/           (not in .gg/ — nothing to migrate)")
	}

	// ── .gitignore alignment ────────────────────────────────────────────────
	gitignorePath := filepath.Join(ggDir, ".gitignore")
	if data, readErr := os.ReadFile(gitignorePath); readErr != nil { //nolint:gosec
		// File missing — write from canonical template.
		if writeErr := os.WriteFile(gitignorePath, []byte(ggGitignoreContent), 0644); writeErr != nil {
			fmt.Printf("  ✗ .gitignore: could not create: %v\n", writeErr)
		} else {
			fmt.Printf("  ✓ .gg/.gitignore created\n")
			migrated = true
		}
	} else {
		content := string(data)
		var missing []string
		for _, line := range gitignoreRequiredLines {
			if !strings.Contains(content, line) {
				missing = append(missing, line)
			}
		}
		if len(missing) > 0 {
			// Append missing entries — preserve existing content.
			f, appendErr := os.OpenFile(gitignorePath, os.O_APPEND|os.O_WRONLY, 0644) //nolint:gosec
			if appendErr != nil {
				fmt.Printf("  ✗ .gitignore: could not update: %v\n", appendErr)
			} else {
				for _, line := range missing {
					_, _ = fmt.Fprintln(f, line)
				}
				_ = f.Close()
				fmt.Printf("  ✓ .gg/.gitignore updated (added: %s)\n", strings.Join(missing, ", "))
				migrated = true
			}
		} else {
			fmt.Println("  ✓ .gg/.gitignore   (aligned — nothing to do)")
		}
	}

	// ── RULES.md template-drift check ──────────────────────────────────────
	rulesPath := filepath.Join(ggDir, "RULES.md")
	rulesStale, rulesErr := isTemplateDrifted(rulesPath, templates.RulesMD)
	switch {
	case rulesErr != nil:
		fmt.Printf("  ⚠ RULES.md: could not read file: %v\n", rulesErr)
	case rulesStale:
		migrated = true
		fmt.Println()
		fmt.Println("RULES.md template drift detected:")
		fmt.Printf("  Current file: %s\n", rulesPath)
		fmt.Printf("  Template:     embedded (internal/templates/rules.md)\n")
		fmt.Println()
		fmt.Print("Re-render .gg/RULES.md from current template? [y/N] ")
		var answer string
		if _, scanErr := fmt.Scanln(&answer); scanErr != nil || strings.ToLower(strings.TrimSpace(answer)) != "y" {
			fmt.Println("Skipped.")
		} else {
			if healErr := healRulesFromTemplate(rulesPath, templates.RulesMD); healErr != nil {
				fmt.Printf("  ✗ RULES.md: re-render failed: %v\n", healErr)
			} else {
				fmt.Printf("  ✓ RULES.md re-rendered from template (backup: %s.bak)\n", rulesPath)
			}
		}
	default:
		fmt.Println("  ✓ RULES.md         (matches current template — nothing to do)")
	}

	fmt.Println()
	if !migrated {
		fmt.Println("Nothing to migrate — runtime state is already in the correct location.")
		return nil
	}

	fmt.Println("Migration complete.")
	if len(gitRmTargets) > 0 {
		fmt.Println()
		fmt.Println("If these paths were tracked by git, un-track them now:")
		fmt.Printf("  git rm --cached %s\n", strings.Join(gitRmTargets, " "))
		fmt.Println()
		fmt.Println("(gg does not commit for you — run the command above manually.)")
	}
	return nil
}

// isTemplateDrifted reports whether the file at path differs from the given
// expected content. Returns false (no drift) when the file doesn't exist.
func isTemplateDrifted(path, expected string) (bool, error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return string(raw) != expected, nil
}

// healRulesFromTemplate backs up path to path+".bak" (overwriting any previous
// backup) then writes expected verbatim.
func healRulesFromTemplate(path, expected string) error {
	bak := path + ".bak"
	if err := os.Rename(path, bak); err != nil {
		return fmt.Errorf("backup %s: %w", path, err)
	}
	if err := os.WriteFile(path, []byte(expected), 0o644); err != nil {
		// Try to restore backup before propagating error.
		_ = os.Rename(bak, path)
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// appendFile reads src and appends its contents to dst (creating dst if needed).
func appendFile(dst, src string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	_, err = f.Write(data)
	return err
}

// copyDir copies a directory tree from src to dst.
func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		data, readErr := os.ReadFile(path) //nolint:gosec
		if readErr != nil {
			return readErr
		}
		return os.WriteFile(target, data, 0o600) //nolint:gosec
	})
}
