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
	DirName    = ".gg"
	ConfigFile = "config.yaml"
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

// FindRoot walks up from cwd looking for the .gg directory.
func FindRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, DirName)); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf(".gg not found — run 'gg init' first")
		}
		dir = parent
	}
}

// GGDir returns the absolute path to the .gg directory.
func GGDir() (string, error) {
	root, err := FindRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, DirName), nil
}

// Load reads the config from .gg/config.yaml and validates it.
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
