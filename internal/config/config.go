package config

import (
	"fmt"
	"os"
	"path/filepath"

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
	Provider string `yaml:"provider"`
	Model    string `yaml:"model"`
	APIKey   string `yaml:"api_key"`
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
			Provider: "openai",
			Model:    "text-embedding-3-small",
			APIKey:   "${OPENAI_API_KEY}",
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

// Load reads the config from .gg/config.yaml.
func Load() (*Config, error) {
	ggDir, err := GGDir()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(ggDir, ConfigFile))
	if err != nil {
		return nil, fmt.Errorf("failed to read config: %w", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}
	// Resolve environment variable references in API key
	if cfg.Embedding.APIKey == "${OPENAI_API_KEY}" || cfg.Embedding.APIKey == "" {
		cfg.Embedding.APIKey = os.Getenv("OPENAI_API_KEY")
	}
	return &cfg, nil
}

// Save writes the config to .gg/config.yaml.
func Save(ggDir string, cfg *Config) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}
	return os.WriteFile(filepath.Join(ggDir, ConfigFile), data, 0644)
}
