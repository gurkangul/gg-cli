// Package session renders the session-start briefing shown to agents when
// they enter a gg shared-memory project — invoked either by an agent's native
// SessionStart hook (installed via `gg doctor --install-agent-hooks`) or
// manually by running `gg session-start`.
//
// The briefing is deliberately short. Full rules live in AGENTS.md; this
// package only reminds agents that durable memory sync exists and where to
// find the current project state.
package session

import (
	"fmt"
	"io"
	"strings"
)

// ProtocolVersion is the marker major version. Tooling that parses the
// briefing should pin to this and accept minor additions. Bump only on
// breaking layout changes to the first-line marker or header keys.
const ProtocolVersion = "v1"

// MarkerPrefix is the stable first-line identifier. Anything downstream
// can grep for this to detect briefing output in mixed streams.
const MarkerPrefix = "gg:session-start:"

// Briefing holds the metadata rendered into the session-start output.
// Agent is required; the rest are optional and omitted when empty.
type Briefing struct {
	Agent       string
	Role        string
	ProjectID   string
	ProjectRoot string
}

// Render writes the briefing to w. Output layout:
//
//	gg:session-start:v1
//	agent: <name>
//	project_id: <uuid>        (optional)
//	project_root: <path>      (optional)
//
//	You are operating inside a gg shared-memory project. Before durable work:
//	  1. Read AGENTS.md …
//	  …
func (b Briefing) Render(w io.Writer) error {
	var sb strings.Builder
	sb.WriteString(MarkerPrefix)
	sb.WriteString(ProtocolVersion)
	sb.WriteByte('\n')
	sb.WriteString("agent: ")
	sb.WriteString(b.Agent)
	sb.WriteByte('\n')
	if strings.TrimSpace(b.Role) != "" {
		sb.WriteString("role: ")
		sb.WriteString(strings.TrimSpace(b.Role))
		sb.WriteByte('\n')
	}
	if b.ProjectID != "" {
		sb.WriteString("project_id: ")
		sb.WriteString(b.ProjectID)
		sb.WriteByte('\n')
	}
	if b.ProjectRoot != "" {
		sb.WriteString("project_root: ")
		sb.WriteString(b.ProjectRoot)
		sb.WriteByte('\n')
	}
	sb.WriteString("\n")
	role := strings.TrimSpace(b.Role)
	if role == "" {
		role = "$GG_ROLE"
	}
	sb.WriteString("You are operating inside a gg-cli shared-memory project. Use gg for durable decisions, evidence, and handoffs.\n")
	sb.WriteString("Next:\n")
	fmt.Fprintf(&sb, "  gg next --agent %s --role %s\n", b.Agent, role)
	sb.WriteString("Fallback:\n")
	fmt.Fprintf(&sb, "  gg inbox --role %s --peek\n", role)
	sb.WriteString("  gg task list --ready --compact\n")
	sb.WriteString("  gg context --compact\n")
	sb.WriteString("Full protocol: docs/agent-protocol-v1.md\n")
	sb.WriteString("\n")
	_, err := io.WriteString(w, sb.String())
	return err
}

// PasteBlock is the copy-paste bootstrap prompt printed at the end of
// `gg init` and `gg doctor` success output. Users paste this into an
// agent's chat to kickstart compliance when no SessionStart hook is
// available (Codex, generic CLIs) or as reinforcement.
//
// agentHint seeds the suggested GG_AGENT value; if empty, "agent" is used.
// The block is plain ASCII — no markdown fences, no emojis —
// so it survives terminal paste across every agent we care about.
func PasteBlock(agentHint string) string {
	if strings.TrimSpace(agentHint) == "" {
		agentHint = "agent"
	}
	var sb strings.Builder
	sb.WriteString("I am operating inside a gg-cli shared-memory project.\n")
	sb.WriteString("Before durable project work:\n")
	sb.WriteString("  1. Choose GG_AGENT for the runtime actually executing gg commands.\n")
	sb.WriteString("     If this shell is GSD, use a unique gsd-* id such as gsd-<project>-1.\n")
	fmt.Fprintf(&sb, "     Suggested for this hook context: %s. Do not copy another runtime's id.\n", agentHint)
	sb.WriteString("  2. Export that chosen value before gg calls; do not continue with a placeholder.\n")
	sb.WriteString("  3. export GG_ROLE=implementer   # role, e.g. implementer/reviewer/planner\n")
	sb.WriteString("  4. Run: gg session-start --agent \"$GG_AGENT\" --role \"$GG_ROLE\"\n")
	sb.WriteString("  5. Run: gg inbox --role \"$GG_ROLE\" --peek\n")
	sb.WriteString("  6. Read AGENTS.md at the repo root.\n")
	sb.WriteString("  7. Use your native workflow, and write anything future agents need\n")
	sb.WriteString("     to gg: decisions, rejections, tasks, bugs, evidence, and handoffs.\n")
	return sb.String()
}
