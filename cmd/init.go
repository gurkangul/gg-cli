package cmd

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/gurkangul/gg/internal/config"
	"github.com/gurkangul/gg/internal/store"
	"github.com/gurkangul/gg/internal/templates"
	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize .gg/ directory with config, docker services, and Qdrant collections",
	RunE:  runInit,
}

func init() {
	rootCmd.AddCommand(initCmd)
}

func runInit(cmd *cobra.Command, args []string) error {
	parentCtx := cmd.Context()
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	ggDir := filepath.Join(cwd, config.DirName)

	// Idempotent: create dirs if missing
	dirs := []string{
		ggDir,
		filepath.Join(ggDir, "volumes", "qdrant"),
		filepath.Join(ggDir, "volumes", "memgraph"),
		filepath.Join(ggDir, "volumes", "ollama"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0755); err != nil {
			return fmt.Errorf("create %s: %w", d, err)
		}
	}

	// Idempotent: write files only if missing
	files := map[string]string{
		"docker-compose.yaml": templates.DockerCompose,
		"config.yaml":         templates.ConfigYAML,
		"RULES.md":            templates.RulesMD,
	}
	for name, content := range files {
		path := filepath.Join(ggDir, name)
		if _, err := os.Stat(path); err == nil {
			fmt.Printf("  .gg/%s already exists, skipping\n", name)
			continue
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			return fmt.Errorf("write %s: %w", name, err)
		}
		fmt.Printf("✓ Created .gg/%s\n", name)
	}

	composePath := filepath.Join(ggDir, "docker-compose.yaml")

	// Start Docker services — 5 min cap.
	fmt.Println("Starting Docker services...")
	composeCtx, cancelCompose := context.WithTimeout(parentCtx, 5*time.Minute)
	defer cancelCompose()
	compose := exec.CommandContext(composeCtx, "docker", "compose", "-f", composePath, "up", "-d")
	compose.Stdout = os.Stdout
	compose.Stderr = os.Stderr
	composeErr := compose.Run()
	if composeErr != nil {
		fmt.Println("⚠ Docker compose failed — start manually: docker compose -f .gg/docker-compose.yaml up -d")
		fmt.Println("\nGG initialized (offline). Run `gg init` again after Docker services are up.")
		return nil
	}
	fmt.Println("✓ Docker services started")

	// Wait for Ollama to respond before pulling the model.
	if err := waitForHTTP(parentCtx, "http://localhost:11434/api/tags", 60*time.Second); err != nil {
		fmt.Println("⚠ Ollama not reachable within 60s — run `ollama pull nomic-embed-text` manually later.")
	} else {
		fmt.Println("Pulling nomic-embed-text model (first time may take a minute)...")
		pullCtx, cancelPull := context.WithTimeout(parentCtx, 10*time.Minute)
		defer cancelPull()
		pull := exec.CommandContext(pullCtx, "docker", "compose", "-f", composePath,
			"exec", "-T", "ollama", "ollama", "pull", "nomic-embed-text")
		pull.Stdout = os.Stdout
		pull.Stderr = os.Stderr
		if err := pull.Run(); err != nil {
			fmt.Println("⚠ Model pull failed — run manually: docker compose -f .gg/docker-compose.yaml exec ollama ollama pull nomic-embed-text")
		} else {
			fmt.Println("✓ nomic-embed-text model ready")
		}
	}

	// Wait for Qdrant and set up collections.
	fmt.Println("Waiting for Qdrant...")
	cfg, cfgErr := config.Load()
	if cfgErr != nil {
		cfg = config.DefaultConfig()
	}
	var client *store.Client
	var healthErr error
	for i := 0; i < 15; i++ {
		if parentCtx.Err() != nil {
			return parentCtx.Err()
		}
		client, healthErr = store.New(&cfg.Qdrant, ggDir)
		if healthErr == nil {
			hctx, hcancel := context.WithTimeout(parentCtx, 2*time.Second)
			healthErr = client.HealthCheck(hctx)
			hcancel()
			if healthErr == nil {
				break
			}
			client.Close()
			client = nil
		}
		time.Sleep(time.Second)
	}
	if healthErr != nil || client == nil {
		fmt.Println("⚠ Qdrant not ready — collections will be created on first use")
		fmt.Println("\nGG initialized! Add .gg/RULES.md to your agent's config.")
		return nil
	}
	defer client.Close()

	setupCtx, cancelSetup := context.WithTimeout(parentCtx, 10*time.Second)
	defer cancelSetup()
	if err := client.EnsureCollections(setupCtx); err != nil {
		return fmt.Errorf("setup collections: %w", err)
	}
	fmt.Println("✓ Qdrant collections ready (decisions, tasks, messages, rejections)")

	fmt.Println("\nGG initialized! Add .gg/RULES.md to your agent's config.")
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
			resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				return nil
			}
		}
		time.Sleep(time.Second)
	}
	return fmt.Errorf("timeout waiting for %s", url)
}
