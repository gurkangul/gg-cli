package cmd

import (
	"context"
	"fmt"
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
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	ggDir := filepath.Join(cwd, config.DirName)

	// Check if already initialized
	if _, err := os.Stat(ggDir); err == nil {
		return fmt.Errorf(".gg/ already exists — already initialized")
	}

	// Create directory structure
	dirs := []string{
		ggDir,
		filepath.Join(ggDir, "volumes", "qdrant"),
		filepath.Join(ggDir, "volumes", "memgraph"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0755); err != nil {
			return fmt.Errorf("create %s: %w", d, err)
		}
	}
	fmt.Println("✓ Created .gg/ directory")

	// Write templates
	files := map[string]string{
		"docker-compose.yaml": templates.DockerCompose,
		"config.yaml":         templates.ConfigYAML,
		"RULES.md":            templates.RulesMD,
	}
	for name, content := range files {
		path := filepath.Join(ggDir, name)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			return fmt.Errorf("write %s: %w", name, err)
		}
		fmt.Printf("✓ Created .gg/%s\n", name)
	}

	// Start Docker services
	fmt.Println("Starting Docker services...")
	compose := exec.Command("docker", "compose", "-f", filepath.Join(ggDir, "docker-compose.yaml"), "up", "-d")
	compose.Stdout = os.Stdout
	compose.Stderr = os.Stderr
	if err := compose.Run(); err != nil {
		fmt.Println("⚠ Docker compose failed — start manually: docker compose -f .gg/docker-compose.yaml up -d")
	} else {
		fmt.Println("✓ Docker services started")
	}

	// Wait for Qdrant to be ready and set up collections
	fmt.Println("Waiting for Qdrant...")
	cfg := config.DefaultConfig()
	var client *store.Client
	for i := 0; i < 15; i++ {
		client, err = store.New(&cfg.Qdrant)
		if err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			err = client.HealthCheck(ctx)
			cancel()
			if err == nil {
				break
			}
			client.Close()
		}
		time.Sleep(time.Second)
	}
	if err != nil {
		fmt.Println("⚠ Qdrant not ready — collections will be created on first use")
		return nil
	}
	defer client.Close()

	ctx := context.Background()
	if err := client.EnsureCollections(ctx); err != nil {
		return fmt.Errorf("setup collections: %w", err)
	}
	fmt.Println("✓ Qdrant collections ready (decisions, tasks, messages, rejections)")

	fmt.Println("\n🧠 GG initialized! Add .gg/RULES.md to your agent's config.")
	return nil
}
