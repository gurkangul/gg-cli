// Package session renders the session-start briefing shown to agents when
// they enter a gg-enforced project — invoked either by an agent's native
// SessionStart hook (installed via `gg doctor --install-agent-hooks`) or
// manually by running `gg session-start`.
//
// The briefing is deliberately short. Full rules live in AGENTS.md; this
// package only reminds agents the protocol exists and where to find it.
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
//	You are operating inside a gg-cli enforced project. Before acting:
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
	sb.WriteString("You are operating inside a gg-cli enforced project. Before acting:\n")
	sb.WriteString("  1. Read AGENTS.md (repo root) — it defines this project's protocol.\n")
	sb.WriteString("  2. Run `gg search --compact <topic>` before proposing anything new.\n")
	sb.WriteString("  3. Record every decision/task/rejection with gg — no exceptions.\n")
	sb.WriteString("  4. Broadcast substantive work via `gg tell all --from <role>`.\n")
	sb.WriteString("  5. Before new work, run `gg inbox` and handle role-targeted assignments — silent skip = violation.\n")
	sb.WriteString("\n")
	_, err := io.WriteString(w, sb.String())
	return err
}

// PasteBlock is the copy-paste bootstrap prompt printed at the end of
// `gg init` and `gg doctor` success output. Users paste this into an
// agent's chat to kickstart compliance when no SessionStart hook is
// available (Codex, generic CLIs) or as reinforcement.
//
// agentHint seeds the example GG_AGENT value; if empty, "agent" is used.
// The block is plain ASCII — no markdown fences, no emojis —
// so it survives terminal paste across every agent we care about.
func PasteBlock(agentHint string) string {
	if strings.TrimSpace(agentHint) == "" {
		agentHint = "agent"
	}
	var sb strings.Builder
	sb.WriteString("I am operating inside a gg-cli enforced project.\n")
	sb.WriteString("Before anything else:\n")
	fmt.Fprintf(&sb, "  1. export GG_AGENT=%s   # use your agent's name\n", agentHint)
	sb.WriteString("  2. Run: gg status\n")
	sb.WriteString("  3. Read AGENTS.md at the repo root.\n")
	sb.WriteString("  4. From now on use gg for every decision, task, rejection,\n")
	sb.WriteString("     and cross-agent handoff. No exceptions.\n")
	return sb.String()
}
