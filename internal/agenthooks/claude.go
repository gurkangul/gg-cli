package agenthooks

import (
	"encoding/json"
	"fmt"
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

// Claude Code hooks schema (as of 2026-04):
//
//	{
//	  "hooks": {
//	    "SessionStart": [
//	      {
//	        "matcher": "startup",
//	        "hooks": [ {"type": "command", "command": "<shell cmd>"} ]
//	      }
//	    ]
//	  }
//	}
//
// The hook command's stdout is injected into the agent's context before
// its first turn, so running `gg session-start` here guarantees the agent
// sees the briefing plus current `gg status` output on every session
// (including post-/clear resumes) without depending on the agent
// remembering to run it.
const (
	claudeEventSessionStart      = "SessionStart"
	claudeEventUserPromptSubmit  = "UserPromptSubmit"
	claudeEventPreToolUse        = "PreToolUse"
	claudeEventPostToolUse       = "PostToolUse"
	claudeMatcherStartup         = "startup"
	claudeMatcherGSDPlan         = "mcp__gsd-workflow__gsd_plan_milestone|mcp__gsd-workflow__gsd_plan_slice|mcp__gsd-workflow__gsd_plan_task|gsd_plan_milestone|gsd_plan_slice|gsd_plan_task"
	claudeMatcherWriteTools      = "Edit|Write|MultiEdit"
	claudeCommand                = "gg session-start --agent=claude-code"
	claudeCommandMarker          = "gg session-start"
	claudeInboxCommand           = "gg inbox --peek"
	claudeInboxCommandMarker     = "gg inbox"
	claudeGSDGuardCommand        = "gg gsd-guard"
	claudeGSDGuardCommandMarker  = "gg gsd-guard"
	// warn-mode: || true so a verify failure never blocks the write, only warns.
	claudeVerifyCommand          = `[ "${GG_NO_VERIFY:-0}" = '1' ] || gg verify --file "$CLAUDE_TOOL_INPUT_FILE_PATH" 2>&1 || true`
	claudeVerifyCommandMarker    = "gg verify --file"
)

type claudeInstaller struct {
	// testHome overrides os.UserHomeDir() in unit tests so global-signal
	// detection doesn't depend on the test machine's actual Claude install.
	// Empty string (the zero value) means "use the real home directory".
	testHome string
	// testEnv overrides os.Getenv in unit tests so env-var signals can be
	// controlled without setting real environment variables. nil = os.Getenv.
	testEnv func(string) string
}

func (c *claudeInstaller) Name() string { return "claude" }
func (c *claudeInstaller) Tier() Tier   { return TierHard }

// hasProjectClaudeDir reports whether this project has a .claude/ directory,
// the primary signal that the project-level hook is (or can be) installed.
func (c *claudeInstaller) hasProjectClaudeDir(projectRoot string) bool {
	info, err := os.Stat(filepath.Join(projectRoot, ".claude"))
	return err == nil && info.IsDir()
}

func (c *claudeInstaller) Detect(projectRoot string) bool {
	// Project-level signal: .claude/ directory exists in the project root.
	if c.hasProjectClaudeDir(projectRoot) {
		return true
	}
	// Global signals: Claude Code is installed on this machine even if this
	// particular project doesn't have a .claude/ directory yet. We still
	// return true so Install can produce an inline suggestion.
	return c.globalSignals()
}

func (c *claudeInstaller) Install(projectRoot string, opts Options) (Result, error) {
	path := pathIn(projectRoot, ".claude", "settings.json")
	res := Result{Path: path}

	// Global install detected but no project-level .claude/ directory — print
	// an inline suggestion rather than auto-creating files the user didn't ask for.
	// Skipped when Force=true: the user explicitly asked us to install.
	if !opts.Force && !c.hasProjectClaudeDir(projectRoot) && c.globalSignals() {
		res.Path = ""
		res.Action = ActionSuggested
		res.Notes = append(res.Notes,
			"global install detected — project hook not yet installed",
			"Run: gg doctor --install-agent-hooks --force --agent claude",
		)
		return res, nil
	}

	data, existed, err := loadJSONFile(path)
	if err != nil {
		return res, err
	}

	sessionStartPresent := claudeHasHook(data, claudeCommandMarker)
	inboxPresent := claudeHasUserPromptHook(data, claudeInboxCommandMarker)
	gsdGuardPresent := claudeHasPreToolUseHook(data, claudeGSDGuardCommandMarker)
	verifyPresent := claudeHasPostToolUseHook(data, claudeVerifyCommandMarker)

	if sessionStartPresent && inboxPresent && gsdGuardPresent && verifyPresent {
		res.Action = ActionUpToDate
		res.Notes = append(res.Notes, "SessionStart hook already present")
		res.Notes = append(res.Notes, "UserPromptSubmit hook already present")
		res.Notes = append(res.Notes, "PreToolUse gsd-guard hook already present")
		res.Notes = append(res.Notes, "PostToolUse verify hook already present")
		return res, nil
	}

	if !sessionStartPresent {
		claudeAddHook(data, claudeCommand)
	}
	if !inboxPresent {
		claudeAddUserPromptHook(data, claudeInboxCommand)
	}
	if !gsdGuardPresent {
		claudeAddPreToolUseHook(data, claudeMatcherGSDPlan, claudeGSDGuardCommand)
	}
	if !verifyPresent {
		claudeAddPostToolUseHook(data, claudeMatcherWriteTools, claudeVerifyCommand)
	}

	if opts.DryRun {
		res.Action = ActionDryRun
		if !sessionStartPresent {
			res.Notes = append(res.Notes, "would add SessionStart hook: "+claudeCommand)
		}
		if !inboxPresent {
			res.Notes = append(res.Notes, "would add UserPromptSubmit hook: "+claudeInboxCommand)
		}
		if !gsdGuardPresent {
			res.Notes = append(res.Notes, "would add PreToolUse gsd-guard hook: "+claudeGSDGuardCommand)
		}
		if !verifyPresent {
			res.Notes = append(res.Notes, "would add PostToolUse verify hook: "+claudeVerifyCommand)
		}
		return res, nil
	}

	if err := writeJSONFile(path, data); err != nil {
		return res, err
	}
	if existed {
		res.Action = ActionUpdated
	} else {
		res.Action = ActionCreated
	}
	if !sessionStartPresent {
		res.Notes = append(res.Notes, "SessionStart hook: "+claudeCommand)
	}
	if !inboxPresent {
		res.Notes = append(res.Notes, "UserPromptSubmit hook: "+claudeInboxCommand)
	}
	if !gsdGuardPresent {
		res.Notes = append(res.Notes, "PreToolUse gsd-guard hook: "+claudeGSDGuardCommand)
	}
	if !verifyPresent {
		res.Notes = append(res.Notes, "PostToolUse verify hook: "+claudeVerifyCommand)
	}
	return res, nil
}

// claudeHasHook reports whether any SessionStart entry already contains a
// command matching marker (substring compare). Used for idempotency.
func claudeHasHook(data map[string]any, marker string) bool {
	hooks, _ := data["hooks"].(map[string]any)
	if hooks == nil {
		return false
	}
	entries, _ := hooks[claudeEventSessionStart].([]any)
	for _, e := range entries {
		m, _ := e.(map[string]any)
		if m == nil {
			continue
		}
		inner, _ := m["hooks"].([]any)
		for _, h := range inner {
			hm, _ := h.(map[string]any)
			if hm == nil {
				continue
			}
			if cmd, _ := hm["command"].(string); strings.Contains(cmd, marker) {
				return true
			}
		}
	}
	return false
}

// claudeHasUserPromptHook reports whether any UserPromptSubmit entry already
// contains a command matching marker (substring compare). Used for idempotency.
func claudeHasUserPromptHook(data map[string]any, marker string) bool {
	hooks, _ := data["hooks"].(map[string]any)
	if hooks == nil {
		return false
	}
	entries, _ := hooks[claudeEventUserPromptSubmit].([]any)
	for _, e := range entries {
		m, _ := e.(map[string]any)
		if m == nil {
			continue
		}
		inner, _ := m["hooks"].([]any)
		for _, h := range inner {
			hm, _ := h.(map[string]any)
			if hm == nil {
				continue
			}
			if cmd, _ := hm["command"].(string); strings.Contains(cmd, marker) {
				return true
			}
		}
	}
	return false
}

// claudeAddUserPromptHook adds a UserPromptSubmit hook entry for cmd. The
// matcher is left empty (matches all prompts) per the Claude Code schema.
func claudeAddUserPromptHook(data map[string]any, cmd string) {
	hooks, _ := data["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
		data["hooks"] = hooks
	}
	entries, _ := hooks[claudeEventUserPromptSubmit].([]any)
	entries = append(entries, map[string]any{
		"matcher": "",
		"hooks": []any{
			map[string]any{"type": "command", "command": cmd},
		},
	})
	hooks[claudeEventUserPromptSubmit] = entries
}

// claudeHasPreToolUseHook reports whether a PreToolUse entry already contains
// a command matching marker (substring compare). Used for idempotency.
func claudeHasPreToolUseHook(data map[string]any, marker string) bool {
	hooks, _ := data["hooks"].(map[string]any)
	if hooks == nil {
		return false
	}
	entries, _ := hooks[claudeEventPreToolUse].([]any)
	for _, e := range entries {
		m, _ := e.(map[string]any)
		if m == nil {
			continue
		}
		inner, _ := m["hooks"].([]any)
		for _, h := range inner {
			hm, _ := h.(map[string]any)
			if hm == nil {
				continue
			}
			if cmd, _ := hm["command"].(string); strings.Contains(cmd, marker) {
				return true
			}
		}
	}
	return false
}

// claudeAddPreToolUseHook adds a PreToolUse hook entry that calls cmd for
// tools whose name matches matcher.
func claudeAddPreToolUseHook(data map[string]any, matcher, cmd string) {
	hooks, _ := data["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
		data["hooks"] = hooks
	}
	entries, _ := hooks[claudeEventPreToolUse].([]any)
	entries = append(entries, map[string]any{
		"matcher": matcher,
		"hooks": []any{
			map[string]any{"type": "command", "command": cmd},
		},
	})
	hooks[claudeEventPreToolUse] = entries
}

// claudeHasPostToolUseHook reports whether a PostToolUse entry already contains
// a command matching marker (substring compare). Used for idempotency.
func claudeHasPostToolUseHook(data map[string]any, marker string) bool {
	hooks, _ := data["hooks"].(map[string]any)
	if hooks == nil {
		return false
	}
	entries, _ := hooks[claudeEventPostToolUse].([]any)
	for _, e := range entries {
		m, _ := e.(map[string]any)
		if m == nil {
			continue
		}
		inner, _ := m["hooks"].([]any)
		for _, h := range inner {
			hm, _ := h.(map[string]any)
			if hm == nil {
				continue
			}
			if cmd, _ := hm["command"].(string); strings.Contains(cmd, marker) {
				return true
			}
		}
	}
	return false
}

// claudeAddPostToolUseHook adds a PostToolUse hook entry that calls cmd for
// tools whose name matches matcher.
func claudeAddPostToolUseHook(data map[string]any, matcher, cmd string) {
	hooks, _ := data["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
		data["hooks"] = hooks
	}
	entries, _ := hooks[claudeEventPostToolUse].([]any)
	entries = append(entries, map[string]any{
		"matcher": matcher,
		"hooks": []any{
			map[string]any{"type": "command", "command": cmd},
		},
	})
	hooks[claudeEventPostToolUse] = entries
}

// claudeAddHook merges a new command hook into data without clobbering
// unrelated keys. If a matcher="startup" entry exists, the new hook is
// appended to its inner hooks list; otherwise a fresh entry is added.
func claudeAddHook(data map[string]any, cmd string) {
	hooks, _ := data["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
		data["hooks"] = hooks
	}
	entries, _ := hooks[claudeEventSessionStart].([]any)

	for i, e := range entries {
		m, _ := e.(map[string]any)
		if m == nil {
			continue
		}
		if matcher, _ := m["matcher"].(string); matcher == claudeMatcherStartup {
			inner, _ := m["hooks"].([]any)
			inner = append(inner, map[string]any{"type": "command", "command": cmd})
			m["hooks"] = inner
			entries[i] = m
			hooks[claudeEventSessionStart] = entries
			return
		}
	}

	entries = append(entries, map[string]any{
		"matcher": claudeMatcherStartup,
		"hooks": []any{
			map[string]any{"type": "command", "command": cmd},
		},
	})
	hooks[claudeEventSessionStart] = entries
}

// loadJSONFile reads path into a map, treating "file missing" and "file
// empty" as starting-from-empty rather than errors. existed reports whether
// a non-empty file was read, so callers can distinguish create vs update.
func loadJSONFile(path string) (data map[string]any, existed bool, err error) {
	raw, readErr := os.ReadFile(path)
	if readErr != nil {
		if os.IsNotExist(readErr) {
			return map[string]any{}, false, nil
		}
		return nil, false, readErr
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return map[string]any{}, true, nil
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, true, fmt.Errorf("parse %s: %w", path, err)
	}
	if data == nil {
		data = map[string]any{}
	}
	return data, true, nil
}

func writeJSONFile(path string, data map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	out, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	if len(out) == 0 || out[len(out)-1] != '\n' {
		out = append(out, '\n')
	}
	return os.WriteFile(path, out, 0o644)
}
