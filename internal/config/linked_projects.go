package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// LinkedProjectConfig declares a read-only project visible to explicit
// --include-linked search/context calls. A scalar value is accepted for compact
// config: UUID-like strings are treated as project IDs; path-like strings are
// resolved to the linked project's .gg/config.yaml.
type LinkedProjectConfig struct {
	ProjectID string `yaml:"project_id,omitempty"`
	Path      string `yaml:"path,omitempty"`
}

func (l *LinkedProjectConfig) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.ScalarNode:
		s := strings.TrimSpace(value.Value)
		if s == "" {
			return nil
		}
		if looksLikePath(s) {
			l.Path = s
		} else {
			l.ProjectID = s
		}
		return nil
	case yaml.MappingNode:
		type plain LinkedProjectConfig
		var p plain
		if err := value.Decode(&p); err != nil {
			return err
		}
		*l = LinkedProjectConfig(p)
		return nil
	default:
		return fmt.Errorf("linked project must be a project ID, path, or mapping")
	}
}

func looksLikePath(s string) bool {
	return strings.HasPrefix(s, ".") ||
		strings.HasPrefix(s, "~") ||
		strings.HasPrefix(s, string(filepath.Separator)) ||
		strings.ContainsAny(s, `/\`)
}

// LoadFromGGDir reads a project-local .gg/config.yaml without changing cwd.
func LoadFromGGDir(ggDir string) (*Config, error) {
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

// ResolveLinkedProject resolves either an explicit project_id or a filesystem
// path to the linked project's ID and .gg directory. Missing projects are
// returned as errors so callers can warn without failing current-project reads.
func ResolveLinkedProject(ref LinkedProjectConfig) (projectID, ggDir string, err error) {
	if strings.TrimSpace(ref.ProjectID) != "" {
		return strings.TrimSpace(ref.ProjectID), "", nil
	}
	if strings.TrimSpace(ref.Path) == "" {
		return "", "", fmt.Errorf("linked project missing project_id or path")
	}
	path, err := expandPath(ref.Path)
	if err != nil {
		return "", "", err
	}
	if filepath.Base(path) != DirName {
		path = filepath.Join(path, DirName)
	}
	cfg, err := LoadFromGGDir(path)
	if err != nil {
		return "", "", err
	}
	return cfg.ProjectID, path, nil
}

func expandPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if strings.HasPrefix(path, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if path == "~" {
			path = home
		} else if strings.HasPrefix(path, "~/") {
			path = filepath.Join(home, path[2:])
		}
	}
	return filepath.Abs(path)
}
