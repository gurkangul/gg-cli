package agenthooks

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

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
	claudeEventSessionStart     = "SessionStart"
	claudeEventUserPromptSubmit = "UserPromptSubmit"
	claudeEventPreToolUse       = "PreToolUse"
	claudeEventPostToolUse      = "PostToolUse"
	claudeEventStop             = "Stop"
	claudeMatcherStartup        = "startup"
	claudeMatcherGSDPlan        = "mcp__gsd-workflow__gsd_plan_milestone|mcp__gsd-workflow__gsd_plan_slice|mcp__gsd-workflow__gsd_plan_task|gsd_plan_milestone|gsd_plan_slice|gsd_plan_task"
	claudeMatcherWriteTools     = "Edit|Write|MultiEdit"
	claudeCommand               = "gg session-start --agent=claude-code"
	claudeCommandMarker         = "gg session-start"
	claudeInboxCommand          = `OUT=$(gg inbox --peek --since-cursor --advance-cursor --role "${GG_ROLE:-}" --include-agents 2>/dev/null); echo "$OUT" | grep -qE 'INBOX \(([1-9][0-9]*) unread\)' && jq -n --arg ctx "$OUT" '{hookSpecificOutput:{hookEventName:"UserPromptSubmit",additionalContext:$ctx}}' || true`
	claudeInboxCommandMarker    = "--since-cursor"
	// claudeInboxStaleMarker matches the old hook command so stale installs can
	// be detected and rewritten to the current format on next `gg doctor`.
	claudeInboxStaleMarker      = "gg inbox"
	claudeGSDGuardCommand       = "gg gsd-guard"
	claudeGSDGuardCommandMarker = "gg gsd-guard"
	// warn-mode: || true so a verify failure never blocks the write, only warns.
	claudeVerifyCommand       = `[ "${GG_NO_VERIFY:-0}" = '1' ] || gg verify --file "$CLAUDE_TOOL_INPUT_FILE_PATH" 2>&1 || true`
	claudeVerifyCommandMarker = "gg verify --file"
	// audit-track: record each Edit/Write/MultiEdit in session audit log.
	claudeAuditTrackCommand       = `[ "${GG_NO_AUDIT:-0}" = '1' ] || gg audit track --session-id "$CLAUDE_SESSION_ID" --file "${CLAUDE_TOOL_INPUT_FILE_PATH:-}" 2>/dev/null || true`
	claudeAuditTrackCommandMarker = "gg audit track"
	// audit-report: emit untracked-mutation warning at session end (non-blocking).
	claudeAuditReportCommand       = `[ "${GG_NO_AUDIT:-0}" = '1' ] || gg audit report --session-id "$CLAUDE_SESSION_ID" 2>/dev/null || true`
	claudeAuditReportCommandMarker = "gg audit report"
)

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

// claudeReplaceUserPromptHook replaces the first UserPromptSubmit hook command
// that contains staleMarker with newCmd. Used to upgrade stale hook installs
// to the current format without leaving duplicate entries.
func claudeReplaceUserPromptHook(data map[string]any, staleMarker, newCmd string) {
	hooks, _ := data["hooks"].(map[string]any)
	if hooks == nil {
		return
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
			if cmd, _ := hm["command"].(string); strings.Contains(cmd, staleMarker) {
				hm["command"] = newCmd
				return
			}
		}
	}
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

// claudeHasStopHook reports whether a Stop entry already contains a command
// matching marker (substring compare). Used for idempotency.
func claudeHasStopHook(data map[string]any, marker string) bool {
	hooks, _ := data["hooks"].(map[string]any)
	if hooks == nil {
		return false
	}
	entries, _ := hooks[claudeEventStop].([]any)
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

// claudeAddStopHook adds a Stop hook entry that calls cmd. The matcher is
// left empty (matches all stop events) per the Claude Code hook schema.
func claudeAddStopHook(data map[string]any, cmd string) {
	hooks, _ := data["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
		data["hooks"] = hooks
	}
	entries, _ := hooks[claudeEventStop].([]any)
	entries = append(entries, map[string]any{
		"matcher": "",
		"hooks": []any{
			map[string]any{"type": "command", "command": cmd},
		},
	})
	hooks[claudeEventStop] = entries
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
