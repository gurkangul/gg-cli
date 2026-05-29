# gg Integration: BMAD + GSD

This document explains how gg-cli, BMAD, and GSD coexist in the same project.

Core rule: BMAD and GSD keep their native workflows. gg stores the durable
memory and evidence that future agents need.

## Why these tools coexist

| Tool | Role |
|---|---|
| **gg-cli** | Cross-agent durable memory — decisions, rejections, shared work items, bugs, evidence, blockers, handoffs. |
| **BMAD** | Skill-based personas for collaborative reasoning, design reviews, PRD/story/architecture work. |
| **GSD** | Optional local scratchpad/helper for specs, context, task planning, and execution notes. |

BMAD outputs and durable GSD outcomes are copied into gg so the full project
memory is available to any agent in any terminal. For compact BMAD and GSD2
capture maps, see [Native Workflow Capture Points](../native-workflow-capture.md).

## Detection signals

`gg doctor --install-agent-hooks` detects the stack automatically:

| Signal | What it means |
|---|---|
| `.gg/` in project root | gg-cli is initialized |
| `_bmad/` in project root | BMAD skills are active |
| `.gsd/gsd.db` in project root | GSD is present |

## Auto-install bridge

Run once per project to wire up both bridge snippets:

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

### What the BMAD installer does

Appends a `<!-- gg-bmad:start/end -->` managed block to `AGENTS.md`. This block
reminds the host agent to persist durable BMAD round outputs, since BMAD skills
run in isolated prompts that usually cannot call gg themselves.

The injected block covers:

- accepted decisions
- rejected approaches
- durable project work proposals
- blockers and handoffs
- artifact/evidence references

It is idempotent — running `--install-agent-hooks` again updates the block
in-place without duplication.

### What the GSD installer does

Appends a `<!-- gg-bridge / /gg-bridge -->` block to `.gsd/KNOWLEDGE.md`. GSD
agents read `KNOWLEDGE.md` at the start of task units, so the gg durable-memory
sync rule is visible without modifying GSD internals.

The injected block provides ready-to-run `gg` commands for decisions, durable
work, blockers, handoffs, evidence summaries, and progress broadcasts.

## Copy durable GSD outcomes into gg

GSD scratchpad items may stay local. When a GSD note becomes durable project
knowledge, copy it into gg:

```sh
gg task create "<short title>" --detail "<scope>" --priority medium --tags "gsd"
gg record "<decision>" --reason "<why>" --tags "gsd"
gg record "<rejected approach>" --decision-status rejected --reason "<why not>" --tags "gsd"
gg tell "all" "<one-line handoff; evidence: commands=<cmds>; live=<smoke>; impact=<files>; gaps=<none|gap>; artifacts=<paths>>" --from gsd --audience agents
```

## BMAD party-mode relay responsibility

The relevant rule is simple:

```text
BMAD rounds may run normally. The host agent copies durable round outputs into gg:
accepted decisions, rejected approaches, shared work items, blockers, artifact
references, and handoffs.
```

Examples:

```sh
gg record "accepted design decision" --reason "why"
gg record "rejected option" --decision-status rejected --reason "why not"
gg task create "follow-up work" --detail "scope" --priority medium --tags "bmad"
gg tell reviewer "Handoff. Evidence: commands run: <cmds>; live smoke: <result>; impacted files: <files>; known gaps: <none|gap>; artifacts: <path>" --from architect --audience agents
```

## Operational decision guide

| Situation | Use |
|---|---|
| Cross-domain design discussion | BMAD party-mode |
| Structured local planning or execution notes | GSD scratchpad/helper |
| Single durable decision, note, bug, or task | gg directly |
| Anything future agents must retrieve | gg as the shared store |

## Sequence: typical mixed-stack session

```text
1. Agent starts → gg status/context reads shared memory
2. User asks for architecture review → BMAD party-mode runs normally
3. Host agent extracts durable outputs → gg record / gg task / gg tell
4. GSD is used for local execution notes, if useful
5. Durable GSD outcomes are copied into gg
6. Any agent searches → gg search "topic" --compact
```

## Related

- [`docs/integrations/gsd.md`](gsd.md) — standalone GSD integration without BMAD
- [`docs/integrations/claude-code.md`](claude-code.md) — Claude Code CLAUDE.md snippet
- [`AGENTS.md`](../../AGENTS.md) — full gg protocol
