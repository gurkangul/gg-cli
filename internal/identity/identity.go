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
// but the process runs inside a Claude Code session that exposes a session id, gg
// derives a unique id "claude-code-<short-session>" so two concurrent tabs on one
// project no longer collapse into one identity — which silently defeated
// task-ownership leases, per-recipient inbox read-state (BUG-082), and verifier
// separation. When no session id is available it degrades to the explicit
// GG_AGENT, preserving existing behavior.
//
// BUG-103: the original code read only CLAUDE_SESSION_ID, but Claude Code exports
// the session id as CLAUDE_CODE_SESSION_ID (CLAUDE_SESSION_ID is unset), so this
// branch was dead and every tab collapsed to the generic id. Read
// CLAUDE_CODE_SESSION_ID first (the real variable), then CLAUDE_SESSION_ID as a
// forward/backward-compatible fallback for other runtimes. Per the recorded
// decision, this per-tab sharpening only lands together with the inbox-gate
// recency window, or a fresh per-tab id would re-block on the entire handoff
// backlog (BUG-102 from the other side).
func ResolveAgent(getenv func(string) string) string {
	agent := strings.TrimSpace(getenv("GG_AGENT"))
	if agent != "" && agent != GenericClaudeAgent {
		return agent // explicit, already-unique identity — respect it.
	}
	sid := strings.TrimSpace(getenv("CLAUDE_CODE_SESSION_ID"))
	if sid == "" {
		sid = strings.TrimSpace(getenv("CLAUDE_SESSION_ID"))
	}
	if sid != "" {
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

// CoarsenAgent reverses the per-tab sharpening ResolveAgent applies, returning
// the runtime name a "claude-code-<short-session>" id was derived from. Any
// other value is returned unchanged.
//
// This exists for DISPLAY aggregation only — grouping an inbox by sender, say,
// where several tabs of one runtime should read as one heading. Never store the
// coarsened form: collapsing distinct sessions into a shared id is precisely
// what BUG-084 was filed to fix, and it silently defeated task leases,
// per-recipient inbox read state and verifier separation.
//
// The prefix check is deliberately exact rather than "strip the last dash
// segment", so a legitimate role like "backend-dev" is never mangled.
func CoarsenAgent(agent string) string {
	rest, ok := strings.CutPrefix(strings.TrimSpace(agent), GenericClaudeAgent+"-")
	if !ok || rest == "" || len(rest) > 8 {
		return agent
	}
	return GenericClaudeAgent
}
