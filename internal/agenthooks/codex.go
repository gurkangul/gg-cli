package agenthooks

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Codex (OpenAI) reads AGENTS.md natively from the repo root. There is no
// runtime hook API — enforcement is strictly soft: the agent reads the
// block but may still ignore its content. We mark our section with stable
// HTML comments so re-runs replace the block in place instead of
// appending duplicates.
const (
	codexFile       = "AGENTS.md"
	codexBlockStart = "<!-- gg-managed:start -->"
	codexBlockEnd   = "<!-- gg-managed:end -->"
)

// codexManagedBody returns the full managed block including fence markers.
// Keep this short — the protocol canonical source is the rest of AGENTS.md,
// not this section. The block's job is to make the rules impossible to miss
// by placing them at a stable, high-visibility location.
func codexManagedBody() string {
	var sb strings.Builder
	sb.WriteString(codexBlockStart)
	sb.WriteString("\n")
	sb.WriteString("## Mandatory gg-cli Protocol (managed by gg — do not edit this block)\n")
	sb.WriteString("\n")
	sb.WriteString("Before acting in this project:\n")
	sb.WriteString("\n")
	sb.WriteString("1. Read this entire AGENTS.md file.\n")
	sb.WriteString("2. Run `gg search --compact <topic>` before proposing anything new.\n")
	sb.WriteString("3. Record every decision/task/rejection with gg — no exceptions.\n")
	sb.WriteString("4. Broadcast substantive work via `gg tell all --from <role>`.\n")
	sb.WriteString("\n")
	sb.WriteString("### GSD ↔ gg mirror (if this project uses GSD)\n")
	sb.WriteString("\n")
	sb.WriteString("GSD is a planning workflow with its own SQLite state in `.gsd/gsd.db`.\n")
	sb.WriteString("Other agents (Claude Code, Cursor, Aider) **cannot read GSD state** — they\n")
	sb.WriteString("only see what's in gg. Without a gg mirror, GSD work is invisible to the\n")
	sb.WriteString("rest of the team.\n")
	sb.WriteString("\n")
	sb.WriteString("GSD itself is **not banned**. It is allowed as a developer execution worker\n")
	sb.WriteString("when the work is created, coordinated, reviewed, and closed in gg. The ban is\n")
	sb.WriteString("only on GSD-owned planning/tracker state becoming canonical.\n")
	sb.WriteString("\n")
	sb.WriteString("Rules when GSD is in use:\n")
	sb.WriteString("\n")
	sb.WriteString("- **Every GSD task (T-level, not milestone or slice) MUST have a gg task\n")
	sb.WriteString("  mirror.** One GSD task = one `gg task create`. Slice/milestone summaries\n")
	sb.WriteString("  are not substitutes — per-task mirroring is the contract.\n")
	sb.WriteString("- Mirror at pickup (`gsd_execute` → `gg task create` first), not at\n")
	sb.WriteString("  completion. Invisible work in flight is the drift class this rule\n")
	sb.WriteString("  prevents.\n")
	sb.WriteString("- Reference the GSD ID in the gg title: `\"[GSD:M001-S02-T05] implement foo\"`\n")
	sb.WriteString("  so anyone in gg can trace back if needed.\n")
	sb.WriteString("- Close the gg mirror with `gg task done` when the GSD task completes.\n")
	sb.WriteString("  Never self-close as implementer — reviewer authority applies here too.\n")
	sb.WriteString("- If you must pick between the two stores, **gg is canonical**. GSD state\n")
	sb.WriteString("  is a planning scratchpad; gg is the shared brain every agent reads.\n")
	sb.WriteString("\n")
	sb.WriteString("**Tracker rule — gg is canonical:** never call\n")
	sb.WriteString("`mcp__gsd-workflow__gsd_plan_milestone`, `gsd_plan_slice`, or `gsd_plan_task`\n")
	sb.WriteString("in a project that uses gg. Those tools write to `.gsd/gsd.db`; gg reads\n")
	sb.WriteString("none of that, so the two stores diverge silently. Use `gg task create` for\n")
	sb.WriteString("every work item and `gg record` for decisions.\n")
	sb.WriteString("\n")
	sb.WriteString("### Gate workflow (check `.gg/config.yaml` for which are active)\n")
	sb.WriteString("\n")
	sb.WriteString("**Task close — when `tasks.require_ready_for_live: true`:**\n")
	sb.WriteString("`gg task done` is refused until the task first transitions via\n")
	sb.WriteString("`gg task ready-for-live TASK-NNN \"verify plan sentence\" --from <your-role>`.\n")
	sb.WriteString("Then close with `gg task done TASK-NNN \"summary\" --verifier <different-role>`.\n")
	sb.WriteString("When `verifier_separation: true`, the verifier role MUST differ from the\n")
	sb.WriteString("actor that set ready_for_live — this blocks single-agent self-certification\n")
	sb.WriteString("(the TASK-200→207 class of premature-close bugs).\n")
	sb.WriteString("\n")
	sb.WriteString("**Bug fix — when `bugs.require_broken_ref: true`:**\n")
	sb.WriteString("`gg bug fix BUG-NNN --repro <path> --repro-broken-ref <SHA>` is mandatory.\n")
	sb.WriteString("The CLI creates a worktree at <SHA> and asserts the repro exits non-zero\n")
	sb.WriteString("there (bug existed), then asserts it exits 0 at HEAD (fix works). A repro\n")
	sb.WriteString("that passes at the broken ref is rejected — it means the test never\n")
	sb.WriteString("exercised the failing path.\n")
	sb.WriteString("\n")
	sb.WriteString("**Before editing any file (mandatory pre-flight):** run `gg impact <file>`\n")
	sb.WriteString("to see historical bugs that have touched it + 1-hop code dependents. Paste\n")
	sb.WriteString("a one-line summary per file into the commit footer.\n")
	sb.WriteString("\n")
	sb.WriteString("**`gg impact` accepts three argument types:**\n")
	sb.WriteString("`<file-path>` → file blast radius; `BUG-NNN` → affected files/symbols;\n")
	sb.WriteString("`TASK-NNN` → downstream dependents via DEPENDS_ON/BLOCKS edges.\n")
	sb.WriteString("\n")
	sb.WriteString("**When reporting a bug, always pass `--affected-files` + `--affected-symbols`**\n")
	sb.WriteString("so the Bug→File AFFECTS edges land in Memgraph. Without them the bug node\n")
	sb.WriteString("exists but is invisible to `gg impact <file>` queries.\n")
	sb.WriteString("\n")
	sb.WriteString(codexBlockEnd)
	sb.WriteString("\n")
	return sb.String()
}

type codexInstaller struct{}

func (c *codexInstaller) Name() string { return "codex" }
func (c *codexInstaller) Tier() Tier   { return TierSoft }
func (c *codexInstaller) ContractPath(projectRoot string) string {
	return pathIn(projectRoot, codexFile)
}

func (c *codexInstaller) Detect(projectRoot string) bool {
	// AGENTS.md is the only reliable signal Codex-class agents will read.
	// `gg init` creates it, so in a gg-bootstrapped project this is
	// effectively always true — which is what we want: the soft injection
	// costs nothing when Codex isn't the active agent, but it's there
	// the moment one shows up.
	_, err := os.Stat(filepath.Join(projectRoot, codexFile))
	return err == nil
}

func (c *codexInstaller) Install(projectRoot string, opts Options) (Result, error) {
	path := pathIn(projectRoot, codexFile)
	res := Result{Path: path, DisplayName: "AGENTS.md"}

	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return res, fmt.Errorf("%s not found — run `gg init` first to create it", codexFile)
		}
		return res, err
	}
	existing := string(raw)
	managed := codexManagedBody()

	updated, changed, mergeErr := replaceOrAppendBlock(existing, codexBlockStart, codexBlockEnd, managed)
	if mergeErr != nil {
		return res, mergeErr
	}
	switch {
	case !changed:
		res.Action = ActionUpToDate
		res.Notes = append(res.Notes, "managed block already current")
	case opts.DryRun:
		res.Action = ActionDryRun
		res.Notes = append(res.Notes, "would update managed block in "+codexFile)
	default:
		if err := os.WriteFile(path, []byte(updated), 0o644); err != nil { //nolint:gosec
			return res, err
		}
		res.Action = ActionUpdated
		res.Notes = append(res.Notes, "managed block written between "+codexBlockStart+" / "+codexBlockEnd)
	}

	// Also write the shared contract block to this file.
	cAction, cNotes, cErr := writeContractBlock(path, opts.DryRun)
	if cErr != nil {
		return res, cErr
	}
	res.Notes = append(res.Notes, cNotes...)
	// Upgrade action to reflect contract write if the main block was already up-to-date.
	if res.Action == ActionUpToDate && cAction != ActionUpToDate {
		res.Action = cAction
	}
	return res, nil
}
