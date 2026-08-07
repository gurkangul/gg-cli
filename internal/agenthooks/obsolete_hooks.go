package agenthooks

import (
	"fmt"
	"path/filepath"
	"strings"
)

// obsoleteHook names a managed hook command gg once installed into an agent's
// settings file and has since retired. Matching is by command substring,
// mirroring claudeHasPreToolUseHook, so an entry is still recognised through
// whatever shell wrapping the installer put around the call.
type obsoleteHook struct {
	path    string
	event   string
	command string
	label   string
}

// obsoleteHooks lists retired managed hook commands.
//
// Deleting the installer is not enough. The command stays behind in every
// settings.json written before the retirement, and a hook pointing at a
// command gg no longer ships fails on every tool call its matcher covers —
// BUG-109: `gg dev-role-guard` is wired to matcher "Bash" and exits 1 on every
// Bash call, so the guard the project thinks it has is not running at all.
// Retiring the writer only stops new installs; this list heals the projects
// that already carry one.
var obsoleteHooks = []obsoleteHook{
	{
		path:    filepath.Join(".claude", "settings.json"),
		event:   claudeEventPreToolUse,
		command: "gg dev-role-guard",
		label:   "dev-role-guard",
	},
}

// RemoveObsoleteHooks strips retired managed hook commands from agent settings
// files. Like RemoveObsoleteBlocks it is deliberately narrow: only exact
// gg-owned commands are removed, and a matcher entry that still carries other
// hooks keeps them and stays.
//
// Best-effort and idempotent — a project with no stale entry is left untouched
// and reports nothing.
func RemoveObsoleteHooks(projectRoot string) ([]string, []error) {
	var lines []string
	var errs []error

	for _, oh := range obsoleteHooks {
		path := filepath.Join(projectRoot, oh.path)
		data, existed, err := loadJSONFile(path)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", path, err))
			continue
		}
		if !existed {
			continue // no settings file — nothing was installed, nothing to retire
		}
		if !removeHookCommand(data, oh.event, oh.command) {
			continue
		}
		if err := writeJSONFile(path, data); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", path, err))
			continue
		}
		lines = append(lines, fmt.Sprintf("  ✓ removed obsolete %s hook from %s", oh.label, oh.path))
	}

	return lines, errs
}

// removeHookCommand deletes every hook whose command contains cmd from the
// named event, pruning a matcher entry once the retired command was the only
// thing it ran, and the event key once no entry survives. Reports whether
// anything was actually removed, so callers only rewrite a changed file.
func removeHookCommand(data map[string]any, event, cmd string) bool {
	hooks, _ := data["hooks"].(map[string]any)
	if hooks == nil {
		return false
	}
	entries, _ := hooks[event].([]any)
	if entries == nil {
		return false
	}

	changed := false
	kept := make([]any, 0, len(entries))
	for _, e := range entries {
		m, _ := e.(map[string]any)
		if m == nil {
			kept = append(kept, e)
			continue
		}
		inner, ok := m["hooks"].([]any)
		if !ok {
			// Shape we do not own — leave it exactly as found.
			kept = append(kept, e)
			continue
		}
		keptInner := make([]any, 0, len(inner))
		for _, h := range inner {
			hm, _ := h.(map[string]any)
			if hm != nil {
				if c, _ := hm["command"].(string); strings.Contains(c, cmd) {
					changed = true
					continue
				}
			}
			keptInner = append(keptInner, h)
		}
		if len(keptInner) == 0 && len(inner) > 0 {
			// This entry existed only to run the retired command, so it goes
			// with it. The len(inner) > 0 guard matters: an entry that ARRIVED
			// with an empty hooks list is somebody's disabled hook, not our
			// leftover, and `changed` is a whole-event flag — without the
			// guard a removal anywhere else in this event would rewrite the
			// file and take that innocent entry along.
			continue
		}
		m["hooks"] = keptInner
		kept = append(kept, m)
	}

	if !changed {
		return false
	}
	if len(kept) == 0 {
		delete(hooks, event)
	} else {
		hooks[event] = kept
	}
	return true
}
