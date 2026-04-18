// Package enforcement centralises the kill-switch for gg's enforcement
// features (agent-hook installers, PreToolUse guard, pre-task-done verify
// gate). Enforcement is ON by default; set GG_ENFORCEMENT=off (or 0/false/no)
// to disable for a session.
package enforcement

import (
	"os"
	"strings"
)

// EnvVar names the opt-out environment variable callers may set to disable
// enforcement for a session.
const EnvVar = "GG_ENFORCEMENT"

// Enabled reports whether enforcement features should run.
// Returns true by default; set GG_ENFORCEMENT=off (or 0/false/no) to disable.
func Enabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvVar))) {
	case "0", "false", "no", "off":
		return false
	}
	return true
}

// DisabledNotice is the human-readable banner printed by commands that
// no-op because enforcement is off. Kept here so every site uses the
// same wording and the user can grep for it.
const DisabledNotice = "enforcement disabled — set " + EnvVar + "=on (or unset) to re-enable"
