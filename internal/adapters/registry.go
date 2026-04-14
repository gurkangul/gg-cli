// Package adapters defines the registry of supported AI coding agents and
// how gg injects its AGENTS.md protocol into each agent's convention file.
package adapters

import (
	"os"
	"path/filepath"
)

// Format controls how gg writes into the target file.
type Format int

const (
	// FullReplace: gg owns the entire file. A header comment marks it as
	// machine-managed. Safe when the file has no user-authored content.
	FullReplace Format = iota

	// ManagedBlock: gg injects between <!-- gg-managed-start --> and
	// <!-- gg-managed-end --> delimiters. Content outside the block is
	// preserved, so users can maintain their own rules alongside gg rules.
	ManagedBlock

	// Symlink: gg creates a symlink pointing to AGENTS.md.
	// The entire file is effectively AGENTS.md.
	Symlink
)

// Adapter describes one AI coding agent and how to inject gg rules into it.
type Adapter struct {
	// Name is the canonical CLI identifier (lowercase, no spaces).
	Name string

	// Targets are the paths (relative to project root) where the agent reads
	// its rules. In order of preference — gg uses the first existing path,
	// or creates the first path if none exist.
	Targets []string

	// Format controls how gg writes into the target file.
	Format Format

	// Role is the GG_ROLE value agents of this type should export.
	Role string

	// DetectFiles are paths (relative to project root or home dir) whose
	// presence suggests this agent is used in the project. Used by --all
	// to auto-detect installed agents.
	DetectFiles []string
}

// All returns the complete registry of supported adapters.
func All() []Adapter {
	return []Adapter{
		{
			Name:        "claude-code",
			Targets:     []string{"CLAUDE.md"},
			Format:      ManagedBlock,
			Role:        "claude-code",
			DetectFiles: []string{".claude", "CLAUDE.md"},
		},
		{
			Name:        "cursor",
			Targets:     []string{".cursor/rules", ".cursorrules"},
			Format:      ManagedBlock,
			Role:        "cursor",
			DetectFiles: []string{".cursor", ".cursorrules"},
		},
		{
			Name:        "aider",
			Targets:     []string{".aider.conf.yml", "CONVENTIONS.md"},
			Format:      ManagedBlock,
			Role:        "aider",
			DetectFiles: []string{".aider.conf.yml", ".aider"},
		},
		{
			Name:        "cline",
			Targets:     []string{".clinerules"},
			Format:      ManagedBlock,
			Role:        "cline",
			DetectFiles: []string{".clinerules", ".cline"},
		},
		{
			Name:        "windsurf",
			Targets:     []string{".windsurfrules"},
			Format:      ManagedBlock,
			Role:        "windsurf",
			DetectFiles: []string{".windsurfrules", ".windsurf"},
		},
		{
			Name:        "roo",
			Targets:     []string{".roo/rules.md", ".roorules"},
			Format:      ManagedBlock,
			Role:        "roo",
			DetectFiles: []string{".roo", ".roorules"},
		},
		{
			Name:        "zed",
			Targets:     []string{".zed/settings.json"},
			Format:      ManagedBlock,
			Role:        "zed",
			DetectFiles: []string{".zed"},
		},
		{
			Name:        "codex",
			Targets:     []string{"AGENTS.md"},
			Format:      Symlink, // codex already reads AGENTS.md natively
			Role:        "codex",
			DetectFiles: []string{},
		},
		{
			Name:        "continue",
			Targets:     []string{".continue/config.json"},
			Format:      ManagedBlock,
			Role:        "continue",
			DetectFiles: []string{".continue"},
		},
	}
}

// ByName returns the adapter with the given name, or nil if not found.
func ByName(name string) *Adapter {
	for _, a := range All() {
		if a.Name == name {
			a := a
			return &a
		}
	}
	return nil
}

// Names returns a sorted list of all adapter names.
func Names() []string {
	all := All()
	names := make([]string, len(all))
	for i, a := range all {
		names[i] = a.Name
	}
	return names
}

// Detect returns adapters whose DetectFiles are present under projectRoot.
func Detect(projectRoot string) []Adapter {
	var detected []Adapter
	for _, a := range All() {
		if isDetected(projectRoot, a.DetectFiles) {
			detected = append(detected, a)
		}
	}
	return detected
}

// ResolveTarget returns the first existing target path (relative to
// projectRoot), or the first path (for creation) if none exist.
func (a *Adapter) ResolveTarget(projectRoot string) string {
	for _, t := range a.Targets {
		p := filepath.Join(projectRoot, t)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if len(a.Targets) > 0 {
		return filepath.Join(projectRoot, a.Targets[0])
	}
	return ""
}

func isDetected(root string, files []string) bool {
	for _, f := range files {
		p := filepath.Join(root, f)
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	return false
}
