package agenthooks

import (
	"os"
	"path/filepath"
	"strings"
)

// claudeGlobalSignalsFromHomeWithEnv reports whether Claude Code appears to
// be installed globally using the given home directory and env lookup function.
// Returns true if ANY of the following are true:
//
//  1. <home>/.claude/settings.json exists and is non-empty
//  2. env var CLAUDECODE == "1"
//  3. env var CLAUDE_CODE_ENTRYPOINT is set (non-empty)
//  4. <home>/.claude/plugins/ is a directory
func claudeGlobalSignalsFromHomeWithEnv(home string, getenv func(string) string) bool {
	// Signal 1: ~/.claude/settings.json non-empty
	raw, readErr := os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	if readErr == nil && len(strings.TrimSpace(string(raw))) > 0 {
		return true
	}
	// Signal 2: CLAUDECODE env var
	if getenv("CLAUDECODE") == "1" {
		return true
	}
	// Signal 3: CLAUDE_CODE_ENTRYPOINT env var
	if getenv("CLAUDE_CODE_ENTRYPOINT") != "" {
		return true
	}
	// Signal 4: ~/.claude/plugins/ directory
	if info, statErr := os.Stat(filepath.Join(home, ".claude", "plugins")); statErr == nil && info.IsDir() {
		return true
	}
	return false
}

// globalSignals calls claudeGlobalSignalsFromHome with the installer's home
// (testHome if set, otherwise os.UserHomeDir()), and uses testEnv for env
// lookup (os.Getenv if nil).
func (c *claudeInstaller) globalSignals() bool {
	home := c.testHome
	if home == "" {
		var err error
		home, err = os.UserHomeDir()
		if err != nil {
			return false
		}
	}
	getenv := c.testEnv
	if getenv == nil {
		getenv = os.Getenv
	}
	return claudeGlobalSignalsFromHomeWithEnv(home, getenv)
}

// hasProjectClaudeDir reports whether this project has a .claude/ directory,
// the primary signal that the project-level hook is (or can be) installed.
func (c *claudeInstaller) hasProjectClaudeDir(projectRoot string) bool {
	info, err := os.Stat(filepath.Join(projectRoot, ".claude"))
	return err == nil && info.IsDir()
}
