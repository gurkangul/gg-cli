package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// ErrMissingProjectID is returned by Validate when project_id is empty —
// typically a pre-refactor config. Callers can detect this with errors.Is
// to provide tailored migration guidance.
var ErrMissingProjectID = errors.New("project_id is required")

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

type MemgraphConfig struct {
	URI      string `yaml:"uri"`      // Bolt URI, e.g. bolt://localhost:7687
	Username string `yaml:"username"` // empty string = no auth (Memgraph default)
	Password string `yaml:"password"` // empty string = no auth
}

type Config struct {
	// ProjectID is a unique per-project UUID used to namespace Qdrant
	// collections. Multiple projects share the same Qdrant instance but see
	// only their own decisions/tasks/messages/rejections.
	ProjectID string          `yaml:"project_id"`
	Qdrant    QdrantConfig    `yaml:"qdrant"`
	Embedding EmbeddingConfig `yaml:"embedding"`
	Memgraph  MemgraphConfig  `yaml:"memgraph"`
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
		Memgraph: MemgraphConfig{
			URI:      "bolt://localhost:7687",
			Username: "",
			Password: "",
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
	// Refuse if the path equals the shared infra dir (including via symlink).
	// Fail closed when HOME is unresolvable: without it we cannot prove the
	// path isn't actually ~/.gg.
	shared, err := SharedDir()
	if err != nil {
		return false
	}
	if SamePath(path, shared) {
		return false
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

// SamePath reports whether two paths resolve to the same filesystem entry.
// It canonicalises via filepath.Abs and filepath.EvalSymlinks when possible;
// paths that don't yet exist fall back to lexical comparison.
func SamePath(a, b string) bool {
	absA, errA := filepath.Abs(a)
	absB, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return false
	}
	if resolved, err := filepath.EvalSymlinks(absA); err == nil {
		absA = resolved
	}
	if resolved, err := filepath.EvalSymlinks(absB); err == nil {
		absB = resolved
	}
	return absA == absB
}

// Load reads the project-local config, applies env var overrides, and validates.
//
// Env var precedence (highest wins):
//
//	MEMGRAPH_PASSWORD — overrides memgraph.password
//	MEMGRAPH_USERNAME — overrides memgraph.username
//	MEMGRAPH_URI      — overrides memgraph.uri
//
// Passwords should never be stored in config.yaml. Leave memgraph.password
// empty and set MEMGRAPH_PASSWORD in the shell environment instead.
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
	applyMemgraphEnvOverrides(&cfg.Memgraph)
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config at %s: %w", path, err)
	}
	return &cfg, nil
}

// applyMemgraphEnvOverrides sets Memgraph credentials from environment variables
// when they are present, so that passwords are never required in config.yaml.
func applyMemgraphEnvOverrides(m *MemgraphConfig) {
	if v := os.Getenv("MEMGRAPH_PASSWORD"); v != "" {
		m.Password = v
	}
	if v := os.Getenv("MEMGRAPH_USERNAME"); v != "" {
		m.Username = v
	}
	if v := os.Getenv("MEMGRAPH_URI"); v != "" {
		m.URI = v
	}
}

// Validate ensures required fields are present and URLs/ports are well-formed.
func (c *Config) Validate() error {
	if strings.TrimSpace(c.ProjectID) == "" {
		return fmt.Errorf("%w — run 'gg init' to generate one", ErrMissingProjectID)
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
