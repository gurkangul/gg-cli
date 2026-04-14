package cmd

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gurkangul/gg/internal/config"
	"github.com/gurkangul/gg/internal/store"
	"github.com/gurkangul/gg/internal/templates"
	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize shared gg infrastructure (~/.gg/) and register this project",
	RunE:  runInit,
}

func init() {
	rootCmd.AddCommand(initCmd)
}

func runInit(cmd *cobra.Command, args []string) error {
	parentCtx := cmd.Context()

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
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	// Refuse to init inside the shared dir or inside an ancestor that already
	// has a .gg/ — that would create a nested project under an existing one,
	// with confusing semantics.
	if err := guardProjectLocation(cwd); err != nil {
		return err
	}

	ggDir := filepath.Join(cwd, config.DirName)
	if err := os.MkdirAll(ggDir, 0755); err != nil {
		return fmt.Errorf("create %s: %w", ggDir, err)
	}

	// Detect legacy per-project docker-compose (pre-refactor setups). Don't
	// silently clobber — guide the user through migration.
	legacyCompose := filepath.Join(ggDir, "docker-compose.yaml")
	if _, err := os.Stat(legacyCompose); err == nil {
		return fmt.Errorf(
			"legacy per-project docker-compose found at %s — stop it with\n"+
				"  docker compose -f %s down -v\n"+
				"then delete %s and re-run `gg init`. Old collection data will not be migrated automatically",
			legacyCompose, legacyCompose, legacyCompose,
		)
	}

	// Generate or preserve project_id
	projectID, err := ensureProjectConfig(ggDir)
	if err != nil {
		return err
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

	if !composeOK {
		fmt.Println("\n⚠ Docker services not running. Project registered with ID", projectID)
		fmt.Println("  Start services manually: docker compose -f ~/.gg/docker-compose.yaml up -d")
		return nil
	}

	// Wait for Qdrant and create this project's collections.
	if err := setupProjectCollections(parentCtx, projectID, ggDir); err != nil {
		return err
	}

	fmt.Printf("\nGG ready. Project %s is registered in shared Qdrant.\n", projectID)
	return nil
}

// ensureProjectConfig creates .gg/config.yaml if missing, generating a fresh
// project_id. Returns the project_id either way.
func ensureProjectConfig(ggDir string) (string, error) {
	configPath := filepath.Join(ggDir, config.ConfigFile)
	if _, err := os.Stat(configPath); err == nil {
		// Already exists — load to extract project_id (do NOT regenerate).
		existing, err := config.Load()
		if err != nil {
			return "", fmt.Errorf("project already initialized but config is invalid: %w", err)
		}
		fmt.Printf("  Project already registered as %s\n", existing.ProjectID)
		return existing.ProjectID, nil
	}
	projectID := uuid.New().String()
	body := strings.ReplaceAll(templates.ConfigYAML, "PROJECT_ID_PLACEHOLDER", projectID)
	if err := os.WriteFile(configPath, []byte(body), 0644); err != nil {
		return "", fmt.Errorf("write config: %w", err)
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
		time.Sleep(time.Second)
	}
	if healthErr != nil || client == nil {
		fmt.Println("⚠ Qdrant not ready — collections will be created on first use")
		return nil
	}
	defer func() { _ = client.Close() }()

	setupCtx, cancelSetup := context.WithTimeout(ctx, 10*time.Second)
	defer cancelSetup()
	if err := client.EnsureCollections(setupCtx); err != nil {
		return fmt.Errorf("setup collections: %w", err)
	}
	fmt.Printf("✓ Qdrant collections ready for project %s\n", projectID)
	return nil
}

// guardProjectLocation refuses to init if cwd is the shared dir itself, or
// if any strict ancestor of cwd already contains a project-local .gg/.
func guardProjectLocation(cwd string) error {
	absCwd, err := filepath.Abs(cwd)
	if err != nil {
		return err
	}
	if shared, err := config.SharedDir(); err == nil {
		if absShared, err := filepath.Abs(shared); err == nil && absCwd == absShared {
			return fmt.Errorf("cannot `gg init` inside the shared dir %s — choose a separate project directory", shared)
		}
	}
	parent := filepath.Dir(absCwd)
	for parent != absCwd {
		if _, err := os.Stat(filepath.Join(parent, config.DirName, config.ConfigFile)); err == nil {
			return fmt.Errorf("ancestor directory %s already contains a gg project — cannot nest a new project under it", parent)
		}
		absCwd = parent
		parent = filepath.Dir(parent)
	}
	return nil
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
		time.Sleep(time.Second)
	}
	return fmt.Errorf("timeout waiting for %s", url)
}
