package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/gurkangul/gg-cli/internal/agenthooks"
	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/templates"
	"github.com/spf13/cobra"
)

// ggGitignoreContent is the canonical .gg/.gitignore written by 'gg init'.
// Runtime state is excluded; brain/ is intentionally tracked (portable snapshot).
const ggGitignoreContent = "# gg-cli runtime state — not committed\n" +
	"cache/\n" +
	"runtime/\n" +
	"*.seq\n" +
	"embedding-meta.json\n" +
	"brain.partial/\n" +
	"\n" +
	"# gg-cli brain snapshot — COMMITTED (portable, git-trackable)\n" +
	"!brain/\n"

// gitignoreRequiredLines are the lines that must appear in .gg/.gitignore.
// Used by 'gg doctor' to detect drift.
var gitignoreRequiredLines = []string{
	"cache/",
	"runtime/",
	"*.seq",
	"embedding-meta.json",
	"brain.partial/",
}

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize shared gg infrastructure (~/.gg/) and register this project",
	RunE:  runInit,
}

var (
	initSkipEnforcement bool
	initWithIndex       bool
	initNoIndex         bool
	initNoIndexHooks    bool
	initYes             bool
)

func init() {
	rootCmd.AddCommand(initCmd)
	initCmd.Flags().BoolVar(&initSkipEnforcement, "skip-enforcement", false,
		"skip installing agent hooks and task-done gate scripts")
	initCmd.Flags().BoolVar(&initWithIndex, "with-index", false,
		"also run `gg index` after setup (non-interactive yes)")
	initCmd.Flags().BoolVar(&initNoIndex, "no-index", false,
		"skip the post-setup index prompt (non-interactive no)")
	initCmd.Flags().BoolVar(&initNoIndexHooks, "no-index-hooks", false,
		"skip auto-installing the CodeGraph git hooks (pre-push/post-merge/post-commit)")
	initCmd.Flags().BoolVar(&initYes, "yes", false,
		"non-interactive: skip prompts")
}

// indexHooksOptOut reports whether the user asked init to skip auto-installing
// the CodeGraph git hooks. Honored sources: the --no-index-hooks flag, the
// GG_NO_INDEX_HOOKS env (truthy), or the config key auto_index_hooks: false.
func indexHooksOptOut() bool {
	if initNoIndexHooks {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(os.Getenv("GG_NO_INDEX_HOOKS"))) {
	case "1", "true", "yes", "on":
		return true
	}
	if cfg, err := config.Load(); err == nil && cfg.AutoIndexHooks != nil && !*cfg.AutoIndexHooks {
		return true
	}
	return false
}

// maybeInstallIndexHooks auto-installs the CodeGraph git hooks after a
// successful init (AC-1). It is idempotent (installGitIndexHooks skips
// already-installed gg hooks) and NON-FATAL: any failure warns on stderr but
// never fails `gg init`. Honors the opt-out (flag/env/config) and is silent
// (no prompt) — GG_YES / non-interactive runs install by default; only an
// explicit opt-out skips. Returns true if hooks are installed afterwards so the
// banner can report accurately.
func maybeInstallIndexHooks(root string) bool {
	if indexHooksOptOut() {
		fmt.Println("  Skipped CodeGraph git hooks (--no-index-hooks). Install later with: gg doctor --install-index-hooks")
		return false
	}
	fmt.Println()
	fmt.Println("CodeGraph Hooks:")
	if err := installGitIndexHooks(root); err != nil {
		fmt.Fprintf(os.Stderr, "⚠ index hooks install (non-fatal): %v\n", err)
		fmt.Fprintln(os.Stderr, "  Install later with: gg doctor --install-index-hooks")
		return false
	}
	return indexHooksInstalled(root)
}

func runInit(cmd *cobra.Command, args []string) error {
	parentCtx := cmd.Context()

	// --- Validate project location BEFORE provisioning anything ---
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	if err := guardProjectLocation(cwd); err != nil {
		return err
	}

	// --- Early language notice (AC-3) ---
	// Detect the project language up front so an unsupported ecosystem is called
	// out BEFORE the slow service provisioning, instead of failing cryptically
	// at the index step. Supported set is go, typescript, python, swift (see
	// internal/index/runner.SupportedLangs); printUnsupportedLangNotice is a
	// no-op when a supported language (or nothing) is detected.
	printUnsupportedLangNotice(cwd)

	// --- Shared infra at ~/.gg/ ---
	// The embedded stores need nothing here. We still write the OPTIONAL
	// docker-compose.yaml (now just the optional Ollama service) + its volume
	// dir so users who prefer Ollama-in-Docker have it ready. Qdrant/Memgraph
	// volume dirs are no longer created — those servers are no longer part of
	// the default compose.
	sharedDir, err := config.SharedDir()
	if err != nil {
		return fmt.Errorf("resolve home dir: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(sharedDir, "volumes", "ollama"), 0755); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Join(sharedDir, "volumes", "ollama"), err)
	}
	composePath := filepath.Join(sharedDir, "docker-compose.yaml")
	f, err := os.OpenFile(composePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	switch {
	case err == nil:
		_, writeErr := f.Write([]byte(templates.DockerCompose))
		closeErr := f.Close()
		if writeErr != nil {
			return fmt.Errorf("write shared docker-compose: %w", writeErr)
		}
		if closeErr != nil {
			return fmt.Errorf("close shared docker-compose: %w", closeErr)
		}
		fmt.Printf("✓ Created %s (optional Ollama-in-Docker service)\n", composePath)
	case os.IsExist(err):
		// Another init wrote it first, or we're re-running — both fine.
	default:
		return fmt.Errorf("create shared docker-compose: %w", err)
	}

	// --- Project-local .gg/ ---
	ggDir := filepath.Join(cwd, config.DirName)
	if err := os.MkdirAll(ggDir, 0755); err != nil {
		return fmt.Errorf("create %s: %w", ggDir, err)
	}

	// Detect legacy pre-refactor artifacts (per-project compose + local volumes).
	// These either collide on ports with the new shared infra or hide gigabytes
	// of orphaned data from the user. Refuse with explicit migration steps.
	legacyCompose := filepath.Join(ggDir, "docker-compose.yaml")
	legacyVolumes := filepath.Join(ggDir, "volumes")
	_, composeErr := os.Stat(legacyCompose)
	_, volumesErr := os.Stat(legacyVolumes)
	if composeErr == nil || volumesErr == nil {
		msg := "legacy per-project setup detected in " + ggDir + ":\n"
		if composeErr == nil {
			msg += "  - docker-compose.yaml (stop: `docker compose -f " + legacyCompose + " down -v`)\n"
		}
		if volumesErr == nil {
			msg += "  - volumes/ (old Qdrant/Memgraph/Ollama data — delete after backing up if needed)\n"
		}
		msg += "remove the above and re-run `gg init`. Old collection data is NOT migrated automatically"
		return fmt.Errorf("%s", msg)
	}

	// Generate or preserve project_id
	projectID, err := ensureProjectConfig(ggDir)
	if err != nil {
		return err
	}

	// Create per-project runtime directory (~/.gg/projects/<projectID>/) so
	// telemetry and cache have a home that is never committed to the repo.
	if initCfg, loadErr := config.Load(); loadErr == nil {
		if _, rtErr := initCfg.RuntimeDir(); rtErr != nil {
			fmt.Printf("⚠ Could not create runtime dir: %v\n", rtErr)
		}
	}

	// --- Backend provisioning (Docker is OPTIONAL) ---
	// With the embedded SQLite stores (the default) no Docker container is
	// required: the vector/graph DBs are created on first use under .gg/.
	// provisionInfra only brings up Docker services that an explicitly-selected
	// SERVER backend (qdrant/memgraph) needs, and warns — never fails — when the
	// embedding endpoint (native Ollama) is unreachable. It returns the resolved
	// config so the rest of init can report accurate, backend-aware guidance.
	infraCfg, infra := provisionInfra(parentCtx, composePath, ggDir)

	// Register this project in ~/.gg/projects.json so `gg system sync` can
	// propagate future contract/hook updates without the user scanning the
	// filesystem for .gg/config.yaml manually. Re-init overwrites the entry.
	if reg, regErr := config.LoadRegistry(); regErr == nil {
		if prevRoot, dup := reg.DuplicateRootFor(projectID, cwd); dup {
			fmt.Printf("⚠ project_id %s was already registered at %s — re-pointing it to %s\n", projectID, prevRoot, cwd)
		}
		reg.Add(projectID, cwd)
		if saveErr := reg.Save(); saveErr != nil {
			fmt.Printf("⚠ registry update failed: %v\n", saveErr)
		}
	}

	// .gg/.gitignore — prevent runtime state from leaking into the repo.
	gitignorePath := filepath.Join(ggDir, ".gitignore")
	if _, err := os.Stat(gitignorePath); err != nil {
		if err := os.WriteFile(gitignorePath, []byte(ggGitignoreContent), 0644); err != nil {
			return fmt.Errorf("write .gg/.gitignore: %w", err)
		}
		fmt.Println("✓ Created .gg/.gitignore")
	}

	// Reference copy of rules for humans reading .gg/
	rulesPath := filepath.Join(ggDir, "RULES.md")
	if _, err := os.Stat(rulesPath); err != nil {
		if err := os.WriteFile(rulesPath, []byte(templates.RulesMD), 0644); err != nil {
			return fmt.Errorf("write RULES.md: %w", err)
		}
		fmt.Println("✓ Created .gg/RULES.md")
	}

	// AGENTS.md at project root — read by GSD, Claude Code, and other agents.
	agentsPath := filepath.Join(cwd, "AGENTS.md")
	if _, err := os.Stat(agentsPath); err != nil {
		if err := os.WriteFile(agentsPath, []byte(templates.AgentsMD), 0644); err != nil {
			return fmt.Errorf("write AGENTS.md: %w", err)
		}
		fmt.Println("✓ Created AGENTS.md at project root")
	} else {
		fmt.Println("  AGENTS.md already exists, skipping (merge gg rules manually if needed)")
	}

	// Bootstrap the vector collections. With the embedded SQLite backend this
	// creates vectorstore.db + the 7 collections with no server. With the qdrant
	// server backend it stays resilient: it polls Qdrant and warns (rather than
	// failing) if unreachable, so a pre-written config / brownfield setup still
	// completes.
	if err := setupProjectCollections(parentCtx, projectID, ggDir); err != nil {
		return err
	}
	printInfraSummary(projectID, infraCfg, infra)

	// Install agent-side hooks so subsequent sessions of any detected agent
	// self-enforce the gg protocol without the user having to remember.
	// Runs regardless of Docker status — hooks only depend on files, and
	// users with Docker down still need the paste prompt to bootstrap.
	//
	// Force=true on init: when the user explicitly runs `gg init` they are
	// asking for project setup — the suggest-only branch in the installer
	// (created for `gg doctor` on existing projects where silent file
	// creation would surprise) is wrong here. Without Force, users who have
	// Claude/Cursor installed globally but no project-local .claude/ dir end
	// up with a suggestion to run a second command, which means TASK-265
	// agent auto-compact + TASK-266 env wiring stay dormant in new projects.
	var installResults []agenthooks.Result
	if !initSkipEnforcement {
		installResults = agenthooks.InstallDetected(cwd, agenthooks.Options{Force: true})
		fmt.Println()
		fmt.Println("Agent Hooks:")
		agenthooks.RenderReport(os.Stdout, installResults)

		// Install task-done gate scripts (.gg/hooks/pre-task-done.d/) so the
		// pre-task-done hook fires on 'gg task done' from the very first session.
		fmt.Println()
		fmt.Println("Task Hooks:")
		if err := runDoctorInstallTaskHooks(); err != nil {
			fmt.Fprintf(os.Stderr, "⚠ task hooks install: %v\n", err)
		}

		// Capture the file-size baseline so 30-file-size.sh grandfathers
		// any pre-existing over-limit files in this project.
		if err := runDoctorSyncBaseline(); err != nil {
			fmt.Fprintf(os.Stderr, "⚠ file-size baseline: %v\n", err)
		}
	}

	// Auto-install the CodeGraph git hooks (AC-1) so the graph self-refreshes on
	// commit/push/merge without a manual `gg index --watch`. Idempotent and
	// non-fatal: a hook-install failure warns but never fails `gg init`.
	hooksInstalled := maybeInstallIndexHooks(cwd)

	langHint := detectLangHint(cwd)
	unsupportedHint := ""
	if langHint == "" {
		unsupportedHint = detectUnsupportedLang(cwd)
	}
	indexed := maybeRunIndex(cmd, langHint, infra.GraphReady)
	printIndexHooksBanner(hooksInstalled)
	printBootstrapPrompt(detectAgentHint(installResults), langHint, unsupportedHint, indexed)
	return nil
}

// printIndexHooksBanner notes (AC-4) that the CodeGraph now auto-refreshes on
// commit/push/merge, or — when the hooks were skipped/failed — how to install
// them. Always foreground git hooks; no background daemon (AC-5).
func printIndexHooksBanner(installed bool) {
	fmt.Println()
	if installed {
		fmt.Println("CodeGraph auto-refresh: ON — git hooks reindex changed files on every commit, push, and merge (foreground, non-blocking; no daemon).")
		return
	}
	fmt.Println("CodeGraph auto-refresh: OFF — install the foreground git hooks anytime with: gg doctor --install-index-hooks")
}

// ensureProjectConfig creates .gg/config.yaml if missing, generating a fresh
// project_id. Returns the project_id either way.
func ensureProjectConfig(ggDir string) (string, error) {
	configPath := filepath.Join(ggDir, config.ConfigFile)
	if info, statErr := os.Stat(configPath); statErr == nil {
		// Recover from a zero-byte config — previous init crashed between
		// O_EXCL create and Write. Treat as "never written", remove and
		// fall through to the create path.
		if info.Size() == 0 {
			if err := os.Remove(configPath); err != nil {
				return "", fmt.Errorf("remove empty config: %w", err)
			}
			fmt.Println("  Removed empty .gg/config.yaml from a prior failed init")
		} else {
			existing, loadErr := config.Load()
			if loadErr != nil {
				if errors.Is(loadErr, config.ErrMissingProjectID) {
					return "", fmt.Errorf(
						"%s looks like a pre-refactor config (no project_id). Delete it to regenerate:\n"+
							"  rm %s && gg init",
						configPath, configPath,
					)
				}
				return "", fmt.Errorf("project already initialized but config is invalid: %w", loadErr)
			}
			fmt.Printf("  Project already registered as %s\n", existing.ProjectID)
			return existing.ProjectID, nil
		}
	}
	// UUID collision risk is ~2^-122; Qdrant collection-create would fail
	// loudly on the astronomically unlikely collision anyway.
	projectID := uuid.New().String()
	body := strings.ReplaceAll(templates.ConfigYAML, "PROJECT_ID_PLACEHOLDER", projectID)
	// O_EXCL to avoid two concurrent gg init's writing different project_ids.
	f, err := os.OpenFile(configPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return "", fmt.Errorf("write config: %w", err)
	}
	if _, err := f.Write([]byte(body)); err != nil {
		_ = f.Close()
		return "", fmt.Errorf("write config: %w", err)
	}
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("close config: %w", err)
	}
	fmt.Printf("✓ Registered project with ID %s\n", projectID)
	return projectID, nil
}

// guardProjectLocation refuses to init if cwd is the shared dir itself, if
// cwd's .gg/ would collide with the shared dir, or if any strict ancestor of
// cwd already contains a gg project. Symlinks are resolved to real paths via
// config.SamePath so symlink-to-home does not bypass the home-collision guard.
func guardProjectLocation(cwd string) error {
	absCwd, err := filepath.Abs(cwd)
	if err != nil {
		return err
	}
	if shared, err := config.SharedDir(); err == nil {
		if config.SamePath(absCwd, shared) {
			return fmt.Errorf("cannot `gg init` inside the shared dir %s — choose a separate project directory", shared)
		}
		// cwd's .gg/ would BE the shared dir (cwd == parent of shared).
		candidate := filepath.Join(absCwd, config.DirName)
		if config.SamePath(candidate, shared) {
			return fmt.Errorf("cannot `gg init` in %s — its .gg/ would collide with the shared infra dir %s", absCwd, shared)
		}
	}
	// We treat os.Stat errors as "no project here" deliberately; a transient
	// EACCES on a remote mount shouldn't abort init in a sibling directory.
	for {
		parent := filepath.Dir(absCwd)
		if parent == absCwd {
			return nil
		}
		if _, err := os.Stat(filepath.Join(parent, config.DirName, config.ConfigFile)); err == nil {
			return fmt.Errorf("ancestor directory %s already contains a gg project — run `gg init` from that directory, or choose a directory outside it", parent)
		}
		absCwd = parent
	}
}
