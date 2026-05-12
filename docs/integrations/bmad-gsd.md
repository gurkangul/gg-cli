# gg Integration: BMAD + GSD

This document explains how `gg-cli`, BMAD (skill-based agents), and GSD (MCP workflow server) coexist in the same project and how to wire them together.

## Why These Three Coexist

| Tool | Role |
|---|---|
| **gg-cli** | Cross-agent memory layer — decisions, tasks, messages, rejections. All agents read and write through `gg`. |
| **BMAD** | Skill-based agent personas (Mary, John, Winston, Amelia, Paige, Sally) for collaborative reasoning, design reviews, and PRD/architecture work. Runs inside Claude Code sessions. |
| **GSD** | Optional local scratchpad/helper for structured execution notes. Its DB-backed state is not canonical. |

gg-cli is the durable persistence layer. BMAD outputs and durable GSD outcomes are copied into gg so that the full project memory is available to any agent in any terminal.

## Detection Signals

`gg doctor --install-agent-hooks` detects the stack automatically:

| Signal | What it means |
|---|---|
| `.gg/` in project root | gg-cli is initialized |
| `_bmad/` in project root | BMAD skills are active |
| `.gsd/gsd.db` in project root | GSD is managing this project |

## Auto-Install Bridge (Recommended)

Run once per project to wire up both bridges:

```sh
# Install BMAD relay block into AGENTS.md
gg doctor --install-agent-hooks --force --agent bmad

# Install gg-cli advisory into .gsd/KNOWLEDGE.md
gg doctor --install-agent-hooks --force --agent gsd
```

Preview changes without writing:

```sh
gg doctor --install-agent-hooks --dry-run --agent bmad
gg doctor --install-agent-hooks --dry-run --agent gsd
```

### What the BMAD Installer Does

Appends a `<!-- gg-bmad:start/end -->` managed block to `AGENTS.md`. This block instructs the Claude Code orchestrator to act as a relay for BMAD subagents, since BMAD skills run in isolated prompts that cannot call `gg` themselves.

The injected block:
- Reminds the orchestrator to call `gg record`, `gg task create`, etc. immediately after each BMAD round
- Covers decisions, task proposals, and rejected approaches
- Is idempotent — running `--install-agent-hooks` again updates the block in-place without duplication

### What the GSD Installer Does

Appends a `<!-- gg-bridge / /gg-bridge -->` block to `.gsd/KNOWLEDGE.md`. GSD agents read `KNOWLEDGE.md` at the start of every task unit, so the gg-as-canonical rule is visible without modifying GSD internals.

The injected block provides ready-to-run `gg` commands for decisions, durable tasks, and progress broadcasts.

## Copy Durable GSD Outcomes Into gg

GSD scratchpad items may stay local. When a GSD note becomes durable project work, copy it into gg:

```sh
gg task create "<short title>" --detail "<scope>" --priority medium --tags "gsd"
gg record "<decision>" --reason "<why>" --tags "gsd"
gg tell "all" "<one-line outcome>" --from gsd --audience agents
```

Bulk import remains available for older GSD projects where you intentionally want to copy existing GSD state into gg:

```sh
# Mirror current project (cwd must contain .gsd/gsd.db)
gg import --from-gsd

# Mirror a different project
gg import --from-gsd --project /path/to/other-project
```

What gets imported:
- **GSD milestones** → gg tasks (priority: high, tagged `gsd-imported,milestone,<id>`)
- **GSD decisions** → gg records (tagged `gsd-imported`; superseded decisions are skipped)
- **GSD slices** → gg notes (tagged `gsd-imported,slice-complete` or `slice-active`)

The import is **idempotent** — each item is tagged with a `gsd-source:<id>` marker. Re-running skips already-imported items.

## BMAD Party-Mode Orchestrator Responsibility

The relevant section from `AGENTS.md` (managed by `gg init`):

```
## SUBAGENTS AND MULTI-AGENT ROUNDS

When you spawn subagents (BMAD party mode, Task-type subagents, role simulations
like Winston/Amelia/John, etc.), those subagents usually cannot invoke gg
themselves — they run in isolated prompts that don't read AGENTS.md.

You, as the orchestrator, are responsible for extracting gg-relevant actions
from their output and executing the gg calls yourself as soon as the round
completes. Concretely:

- A subagent says "we should reject X because Y" → gg record "X" --decision-status=rejected --reason "Y"
- A subagent proposes action items / a punch list → gg task create for each
- A subagent reaches a conclusion the user accepts → gg record "conclusion" --reason "..."

Do this BEFORE asking the user "should I save these?" — capture decisions automatically.
```

## Operational Decision Guide

| Situation | Use |
|---|---|
| Cross-domain design discussion (PM + architect + analyst) | BMAD party-mode |
| Multi-week structured roadmap execution | gg tasks as canonical; GSD may be a manual scratchpad/helper |
| Single decision, note, or ad-hoc task | `gg` directly |
| All of the above — persistence | `gg` as the shared store |

## Sequence: Typical Mixed-Stack Session

```
1. Claude Code starts → gg status (reads shared memory)
2. User asks for architecture review → BMAD party-mode (Mary + Winston)
3. Claude Code extracts decisions → gg record / gg task create
4. GSD is used manually for local execution notes, if useful
5. Durable GSD outcomes are copied into gg → gg task create / gg record / gg tell
6. gg import --from-gsd (optional: bulk copy existing GSD state into gg)
7. Any agent searches → gg search "topic" --compact
```

## Related

- [`docs/integrations/gsd.md`](gsd.md) — standalone GSD integration without BMAD
- [`docs/integrations/claude-code.md`](claude-code.md) — Claude Code CLAUDE.md snippet
- [`AGENTS.md`](../../AGENTS.md) — full gg protocol (managed by `gg init`)
