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
	claudeEventSessionStart = "SessionStart"
	claudeMatcherStartup    = "startup"
	claudeCommand           = "gg session-start --agent=claude-code"
	// claudeCommandMarker is the substring used to detect a pre-existing
	// gg hook during idempotent merges. Matching on the whole command
	// string would be fragile if the user tweaks flags; matching the
	// distinctive `gg session-start` prefix is enough.
	claudeCommandMarker = "gg session-start"
)

type claudeInstaller struct{}

func (c *claudeInstaller) Name() string { return "claude" }
func (c *claudeInstaller) Tier() Tier   { return TierHard }

func (c *claudeInstaller) Detect(projectRoot string) bool {
	// Presence of .claude/ (a project-local settings/agent config dir)
	// is a reliable signal the user is running Claude Code in this repo.
	info, err := os.Stat(filepath.Join(projectRoot, ".claude"))
	return err == nil && info.IsDir()
}

func (c *claudeInstaller) Install(projectRoot string, opts Options) (Result, error) {
	path := pathIn(projectRoot, ".claude", "settings.json")
	res := Result{Path: path}

	data, existed, err := loadJSONFile(path)
	if err != nil {
		return res, err
	}

	if claudeHasHook(data, claudeCommandMarker) {
		res.Action = ActionUpToDate
		res.Notes = append(res.Notes, "SessionStart hook already present")
		return res, nil
	}

	claudeAddHook(data, claudeCommand)

	if opts.DryRun {
		res.Action = ActionDryRun
		res.Notes = append(res.Notes, "would add SessionStart hook: "+claudeCommand)
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
	res.Notes = append(res.Notes, "SessionStart hook: "+claudeCommand)
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
