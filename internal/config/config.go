package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	DirName       = ".gg"
	ConfigFile    = "config.yaml"
	SharedDirName = ".gg" // ~/.gg/ for shared infrastructure
)

type QdrantConfig struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

type EmbeddingConfig struct {
	Host  string `yaml:"host"`
	Model string `yaml:"model"`
}

type Config struct {
	// ProjectID is a unique per-project UUID used to namespace Qdrant
	// collections. Multiple projects share the same Qdrant instance but see
	// only their own decisions/tasks/messages/rejections.
	ProjectID string          `yaml:"project_id"`
	Qdrant    QdrantConfig    `yaml:"qdrant"`
	Embedding EmbeddingConfig `yaml:"embedding"`
}

func DefaultConfig() *Config {
	return &Config{
		Qdrant: QdrantConfig{
			Host: "localhost",
			Port: 6334,
		},
		Embedding: EmbeddingConfig{
			Host:  "http://localhost:11434",
			Model: "nomic-embed-text",
		},
	}
}

// FindRoot walks up from cwd looking for a project-local .gg directory.
func FindRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if isProjectGGDir(filepath.Join(dir, DirName)) {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf(".gg not found — run 'gg init' first")
		}
		dir = parent
	}
}

// isProjectGGDir reports whether the given path is a project-local .gg dir
// (contains config.yaml), as opposed to the shared ~/.gg/ infra dir.
// It explicitly refuses the shared dir even if the user somehow created a
// config.yaml there, preventing accidental data writes to home.
func isProjectGGDir(path string) bool {
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return false
	}
	if shared, err := SharedDir(); err == nil {
		if abs, err := filepath.Abs(path); err == nil {
			if absShared, err := filepath.Abs(shared); err == nil && abs == absShared {
				return false
			}
		}
	}
	_, err = os.Stat(filepath.Join(path, ConfigFile))
	return err == nil
}

// GGDir returns the absolute path to the project-local .gg directory.
func GGDir() (string, error) {
	root, err := FindRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, DirName), nil
}

// SharedDir returns the absolute path to ~/.gg/ — the shared infrastructure
// directory holding docker-compose.yaml and service volumes.
func SharedDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, SharedDirName), nil
}

// Load reads the project-local config and validates it.
func Load() (*Config, error) {
	ggDir, err := GGDir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(ggDir, ConfigFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config: %w", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", path, err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config at %s: %w", path, err)
	}
	return &cfg, nil
}

// Validate ensures required fields are present and URLs/ports are well-formed.
func (c *Config) Validate() error {
	if strings.TrimSpace(c.ProjectID) == "" {
		return fmt.Errorf("project_id is required — run 'gg init' to generate one")
	}
	if strings.TrimSpace(c.Qdrant.Host) == "" {
		return fmt.Errorf("qdrant.host is required")
	}
	if c.Qdrant.Port <= 0 || c.Qdrant.Port > 65535 {
		return fmt.Errorf("qdrant.port must be 1..65535, got %d", c.Qdrant.Port)
	}
	if strings.TrimSpace(c.Embedding.Host) == "" {
		return fmt.Errorf("embedding.host is required")
	}
	u, err := url.Parse(c.Embedding.Host)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("embedding.host must be a full URL including scheme (http:// or https://), got %q", c.Embedding.Host)
	}
	if strings.TrimSpace(c.Embedding.Model) == "" {
		return fmt.Errorf("embedding.model is required")
	}
	return nil
}
