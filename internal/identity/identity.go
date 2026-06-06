// Package identity resolves the effective per-runtime agent identity.
package identity

import (
	"os"
	"strings"
)

// GenericClaudeAgent is the static value gg's installer writes into Claude
// Code's settings.json env. It is shared by every Claude tab on a project, so
// on its own it cannot distinguish concurrent sessions (BUG-084).
const GenericClaudeAgent = "claude-code"

// ResolveAgent returns the effective per-runtime agent identity.
//
// BUG-084: when GG_AGENT is the generic shared "claude-code" default (or unset)
// but the process runs inside a Claude Code session that exposes
// CLAUDE_SESSION_ID, gg derives a unique id "claude-code-<short-session>" so two
// concurrent tabs on one project no longer collapse into one identity — which
// silently defeated task-ownership leases, per-recipient inbox read-state
// (BUG-082), and verifier separation. When no session id is available it
// degrades to the explicit GG_AGENT, preserving existing behavior.
func ResolveAgent(getenv func(string) string) string {
	agent := strings.TrimSpace(getenv("GG_AGENT"))
	if agent != "" && agent != GenericClaudeAgent {
		return agent // explicit, already-unique identity — respect it.
	}
	if sid := strings.TrimSpace(getenv("CLAUDE_SESSION_ID")); sid != "" {
		short := sid
		if len(short) > 8 {
			short = short[:8]
		}
		return GenericClaudeAgent + "-" + short
	}
	return agent // "claude-code" or "" — no session signal to disambiguate.
}

// Agent is ResolveAgent bound to the real process environment.
func Agent() string { return ResolveAgent(os.Getenv) }
