package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gurkangul/gg-cli/internal/agenthooks"
	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/session"
	"github.com/gurkangul/gg-cli/internal/store"
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
)

func init() {
	rootCmd.AddCommand(initCmd)
	initCmd.Flags().BoolVar(&initSkipEnforcement, "skip-enforcement", false,
		"skip installing agent hooks and task-done gate scripts")
	initCmd.Flags().BoolVar(&initWithIndex, "with-index", false,
		"also run `gg index` after setup (non-interactive yes)")
	initCmd.Flags().BoolVar(&initNoIndex, "no-index", false,
		"skip the post-setup index prompt (non-interactive no)")
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

	// --- Shared infra at ~/.gg/ ---
	sharedDir, err := config.SharedDir()
	if err != nil {
		return fmt.Errorf("resolve home dir: %w", err)
	}
	sharedVolumes := []string{
		filepath.Join(sharedDir, "volumes", "qdrant"),
		filepath.Join(sharedDir, "volumes", "memgraph"),
		filepath.Join(sharedDir, "volumes", "ollama"),
	}
	for _, d := range sharedVolumes {
		if err := os.MkdirAll(d, 0755); err != nil {
			return fmt.Errorf("create %s: %w", d, err)
		}
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
		fmt.Printf("✓ Created %s (shared infrastructure)\n", composePath)
	case os.IsExist(err):
		// Another init wrote it first, or we're re-running — both fine.
	default:
		return fmt.Errorf("create shared docker-compose: %w", err)
	}

	composeOK := startSharedServices(parentCtx, composePath)
	ollamaHost := config.DefaultConfig().Embedding.Host
	if composeOK {
		pullOllamaModel(parentCtx, composePath, ollamaHost)
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

	// Register this project in ~/.gg/projects.json so `gg system sync` can
	// propagate future contract/hook updates without the user scanning the
	// filesystem for .gg/config.yaml manually. Re-init overwrites the entry.
	if reg, regErr := config.LoadRegistry(); regErr == nil {
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

	if composeOK {
		// Wait for Qdrant and create this project's collections.
		if err := setupProjectCollections(parentCtx, projectID, ggDir); err != nil {
			return err
		}
		fmt.Printf("\nGG ready. Project %s is registered in shared Qdrant.\n", projectID)
	} else {
		fmt.Println("\n⚠ Docker services not running. Project registered with ID", projectID)
		fmt.Println("  Start services manually: docker compose -f ~/.gg/docker-compose.yaml up -d")
	}

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

	langHint := detectLangHint(cwd)
	indexed := maybeRunIndex(cmd, langHint, composeOK)
	printBootstrapPrompt(detectAgentHint(installResults), langHint, indexed)
	return nil
}

// detectAgentHint picks the most likely "current runtime agent" from the
// install report so the paste-block example uses the right GG_AGENT value.
// First successful HARD-tier install wins; falls back to claude-code.
func detectAgentHint(results []agenthooks.Result) string {
	for _, r := range results {
		if r.Tier != agenthooks.TierHard {
			continue
		}
		if r.Action == agenthooks.ActionCreated || r.Action == agenthooks.ActionUpdated {
			switch r.AgentName {
			case "claude":
				return "claude-code"
			case "cursor":
				return "cursor"
			}
		}
	}
	return "claude-code"
}

// detectLangHint returns the most likely index language based on files in cwd.
func detectLangHint(dir string) string {
	checks := []struct {
		file string
		lang string
	}{
		{"go.mod", "go"},
		{"package.json", "typescript"},
		{"pyproject.toml", "python"},
		{"setup.py", "python"},
		{"requirements.txt", "python"},
	}
	for _, c := range checks {
		if _, err := os.Stat(filepath.Join(dir, c.file)); err == nil {
			return c.lang
		}
	}
	return "go"
}

// printBootstrapPrompt emits the paste-block users copy into their agent's
// chat to trigger gg protocol compliance on first use. When a SessionStart
// hook is also installed, the block is reinforcement; for agents without a
// hook surface (Codex, Zai), it is the primary handoff.
func printBootstrapPrompt(agentHint, langHint string, indexed bool) {
	fmt.Println()
	fmt.Println("Paste this into your AI agent's chat (works for any agent):")
	fmt.Println()
	fmt.Println("────────────────────────────────────────────────────────────")
	fmt.Print(session.PasteBlock(agentHint))
	fmt.Println("────────────────────────────────────────────────────────────")
	fmt.Println()
	if !indexed {
		fmt.Printf("Next: run `gg index --lang %s` to populate the code graph.\n", langHint)
		fmt.Println()
	}
	fmt.Println("Forgot the prompt? Run `gg doctor` — it shows it again.")
	fmt.Println("Re-install hooks later: `gg doctor --install-agent-hooks`.")
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

func startSharedServices(ctx context.Context, composePath string) bool {
	fmt.Println("Starting shared Docker services...")
	composeCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	compose := exec.CommandContext(composeCtx, "docker", "compose", "-f", composePath, "up", "-d")
	compose.Stdout = os.Stdout
	compose.Stderr = os.Stderr
	if err := compose.Run(); err != nil {
		fmt.Println("⚠ Docker compose failed — start manually: docker compose -f", composePath, "up -d")
		return false
	}
	fmt.Println("✓ Shared Docker services running")
	return true
}

func pullOllamaModel(ctx context.Context, composePath, ollamaHost string) {
	tagsURL := strings.TrimRight(ollamaHost, "/") + "/api/tags"
	if err := waitForHTTP(ctx, tagsURL, 60*time.Second); err != nil {
		fmt.Println("⚠ Ollama not reachable within 60s — pull model manually:")
		fmt.Println("  docker compose -f", composePath, "exec ollama ollama pull nomic-embed-text")
		return
	}
	fmt.Println("Pulling nomic-embed-text model (first time only, shared across all projects)...")
	pullCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	pull := exec.CommandContext(pullCtx, "docker", "compose", "-f", composePath,
		"exec", "-T", "ollama", "ollama", "pull", "nomic-embed-text")
	pull.Stdout = os.Stdout
	pull.Stderr = os.Stderr
	if err := pull.Run(); err != nil {
		fmt.Println("⚠ Model pull failed — retry manually: docker compose -f", composePath, "exec ollama ollama pull nomic-embed-text")
		return
	}
	fmt.Println("✓ nomic-embed-text model ready")
}

func setupProjectCollections(ctx context.Context, projectID, ggDir string) error {
	fmt.Println("Waiting for Qdrant...")
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load freshly-written project config: %w", err)
	}
	var client *store.Client
	var healthErr error
	for i := 0; i < 15; i++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		client, healthErr = store.New(&cfg.Qdrant, ggDir, projectID)
		if healthErr == nil {
			hctx, hcancel := context.WithTimeout(ctx, 2*time.Second)
			healthErr = client.HealthCheck(hctx)
			hcancel()
			if healthErr == nil {
				break
			}
			_ = client.Close()
			client = nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	if healthErr != nil || client == nil {
		fmt.Println("⚠ Qdrant not ready — collections will be created on first use")
		return nil
	}
	defer func() { _ = client.Close() }()

	setupCtx, cancelSetup := context.WithTimeout(ctx, 10*time.Second)
	defer cancelSetup()
	if err := client.EnsureCollections(setupCtx, store.VectorSize); err != nil {
		return fmt.Errorf("setup collections: %w", err)
	}
	fmt.Printf("✓ Qdrant collections ready for project %s\n", projectID)
	return nil
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

// waitForHTTP polls a URL with GET until a 2xx response or timeout.
func waitForHTTP(ctx context.Context, url string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 2 * time.Second}
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return fmt.Errorf("timeout waiting for %s", url)
}

