// Package enforcement centralises the kill-switch for gg's enforcement
// features (agent-hook installers, PreToolUse guard, pre-task-done verify
// gate). During the v0.3 stabilization window these features default to
// OFF so we can measure the meta/feature ratio without the self-feeding
// backlog loop described in the Mary-plan decision.
package enforcement

import "os"

// EnvVar names the opt-in environment variable callers must set to
// re-enable enforcement during stabilization.
const EnvVar = "GG_ENFORCEMENT"

// Enabled reports whether enforcement features should run.
// Set GG_ENFORCEMENT=1 (or "true"/"yes") to opt in.
func Enabled() bool {
	switch os.Getenv(EnvVar) {
	case "1", "true", "TRUE", "yes", "YES", "on", "ON":
		return true
	}
	return false
}

// DisabledNotice is the human-readable banner printed by commands that
// no-op because enforcement is off. Kept here so every site uses the
// same wording and the user can grep for it.
const DisabledNotice = "enforcement disabled — set " + EnvVar + "=1 to enable (stabilization window)"
