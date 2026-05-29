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
	sb.WriteString("## gg Durable Memory Protocol (managed by gg — do not edit this block)\n")
	sb.WriteString("\n")
	sb.WriteString("gg-cli does not own the agent's workflow. Use the native workflow that fits\n")
	sb.WriteString("the work, and sync durable outputs into gg so future agents can retrieve them.\n")
	sb.WriteString("\n")
	sb.WriteString("Durable outputs include decisions, rejected approaches, shared work items,\n")
	sb.WriteString("bugs/root causes, blockers, handoffs, evidence summaries, and artifact references.\n")
	sb.WriteString("Evidence summaries should stay compact: commands run, live smoke result, impacted files, known gaps, and artifact paths.\n")
	sb.WriteString("\n")
	sb.WriteString("Recommended orientation:\n")
	sb.WriteString("\n")
	sb.WriteString("1. Read this entire AGENTS.md file.\n")
	sb.WriteString("2. Run `gg session-start --agent <agent_id> --role <role>` when available.\n")
	sb.WriteString("3. Run `gg search --compact <topic>` before changing important behavior.\n")
	sb.WriteString("4. Record durable decisions/rejections/evidence with `gg record`, `gg task`, `gg bug`, or `gg tell`.\n")
	sb.WriteString("\n")
	sb.WriteString("### GSD usage (if this project uses GSD)\n")
	sb.WriteString("\n")
	sb.WriteString("GSD is a native planning or execution workflow with its own SQLite state in `.gsd/gsd.db`.\n")
	sb.WriteString("Other agents (Claude Code, Cursor, Aider) cannot read that state; they only\n")
	sb.WriteString("see what is written to gg. Treat GSD as a local scratchpad/helper, not shared memory.\n")
	sb.WriteString("\n")
	sb.WriteString("GSD itself is allowed. Run it when useful, and copy durable outcomes into gg.\n")
	sb.WriteString("\n")
	sb.WriteString("Rules when GSD is in use:\n")
	sb.WriteString("\n")
	sb.WriteString("- Create gg tasks only for durable work items that future agents need.\n")
	sb.WriteString("- Use `gg record` for decisions and rejected approaches.\n")
	sb.WriteString("- Use `gg tell` for cross-agent handoffs and blockers.\n")
	sb.WriteString("- Summarize useful GSD output back into gg; do not rely on `.gsd/gsd.db` for shared memory.\n")
	sb.WriteString("\n")
	sb.WriteString("**Shared-memory rule:** do not use `mcp__gsd-workflow__gsd_plan_milestone`,\n")
	sb.WriteString("`gsd_plan_slice`, or `gsd_plan_task` as the durable memory source in a project that\n")
	sb.WriteString("uses gg. Those tools write to `.gsd/gsd.db`; gg reads none of that, so the\n")
	sb.WriteString("state is invisible to other agents unless durable outcomes are mirrored into gg.\n")
	sb.WriteString("\n")
	sb.WriteString("### Configured evidence gates\n")
	sb.WriteString("\n")
	sb.WriteString("Projects may configure review/evidence gates around `gg task done` and `gg bug fix`.\n")
	sb.WriteString("These gates protect durable memory quality; they do not require a particular native workflow.\n")
	sb.WriteString("\n")
	sb.WriteString("**Task close — when `tasks.require_ready_for_live: true`:**\n")
	sb.WriteString("`gg task done` is refused until the task first transitions via\n")
	sb.WriteString("`gg task ready-for-live TASK-NNN --plan \"Reviewer: inspect diff and rerun smoke. Evidence: commands=<cmds>; live=<smoke>; impact=<files>; gaps=<none|gap>; artifacts=<paths>\" --from <your-role>`.\n")
	sb.WriteString("Then close with `gg task done TASK-NNN \"summary\" --verifier <different-role>`.\n")
	sb.WriteString("When `verifier_separation: true`, closure also needs durable verification\n")
	sb.WriteString("evidence from a different role than the actor that set ready_for_live.\n")
	sb.WriteString("\n")
	sb.WriteString("**Bug fix — when `bugs.require_broken_ref: true`:**\n")
	sb.WriteString("`gg bug fix BUG-NNN --repro <path> --repro-broken-ref <SHA>` verifies the\n")
	sb.WriteString("repro fails at the broken ref and passes at HEAD, proving the bug existed and the fix works.\n")
	sb.WriteString("\n")
	sb.WriteString("**Before source edits where impact matters:** run `gg impact <file>` to see\n")
	sb.WriteString("historical bugs, dependents, exported symbols, and related decisions. Capture\n")
	sb.WriteString("the impact/evidence summary where future agents can find it.\n")
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
