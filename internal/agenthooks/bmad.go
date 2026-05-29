package agenthooks

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// BMAD skill agents (Mary, John, Winston, Amelia, Paige, Sally) run inside
// Claude Code sessions via the BMAD skill framework. They cannot invoke gg
// as a subprocess themselves — the host agent must extract durable outputs
// (decisions, tasks, rejections, blockers, artifact references, handoffs) and
// call gg directly.
//
// The installer injects a BMAD-specific guidance section into AGENTS.md so
// the host agent (Claude Code reading AGENTS.md) sees the relay rule for its
// BMAD subagents.
//
// Tier: SOFT — the block is readable by the host agent but durable capture
// depends on that agent following the instruction.
const (
	bmadFile       = "AGENTS.md"
	bmadBlockStart = "<!-- gg-bmad:start -->"
	bmadBlockEnd   = "<!-- gg-bmad:end -->"
)

func bmadManagedBody() string {
	var sb strings.Builder
	sb.WriteString(bmadBlockStart)
	sb.WriteString("\n")
	sb.WriteString("## BMAD Skill Agents — gg Durable Memory Relay\n")
	sb.WriteString("\n")
	sb.WriteString("BMAD rounds may run normally. gg-cli does not own BMAD's workflow; it only\n")
	sb.WriteString("stores durable outputs that future agents need.\n")
	sb.WriteString("\n")
	sb.WriteString("BMAD agents (Mary, John, Winston, Amelia, Paige, Sally, and others) run\n")
	sb.WriteString("inside Claude Code sessions. They usually cannot call gg directly. The host\n")
	sb.WriteString("agent must persist durable round outputs into gg:\n")
	sb.WriteString("\n")
	sb.WriteString("- After each BMAD round: extract accepted decisions, task proposals, blockers,\n")
	sb.WriteString("  compact evidence summaries, artifact references, handoffs, and rejected approaches that future agents need.\n")
	sb.WriteString("- If a BMAD agent says 'reject X' → `gg record \"X\" --decision-status=rejected --reason \"why\"`\n")
	sb.WriteString("- If a BMAD agent proposes durable project work → `gg task create \"title\" ...`\n")
	sb.WriteString("- If a BMAD agent reaches a conclusion the user accepts → `gg record \"conclusion\" --reason \"...\"`\n")
	sb.WriteString("\n")
	sb.WriteString("Do this before moving on; otherwise the knowledge stays trapped in one prompt.\n")
	sb.WriteString("\n")
	sb.WriteString(bmadBlockEnd)
	sb.WriteString("\n")
	return sb.String()
}

type bmadInstaller struct{}

func (b *bmadInstaller) Name() string { return "bmad" }
func (b *bmadInstaller) Tier() Tier   { return TierSoft }
func (b *bmadInstaller) ContractPath(projectRoot string) string {
	return pathIn(projectRoot, bmadFile)
}

func (b *bmadInstaller) Detect(projectRoot string) bool {
	// _bmad/ directory is the canonical signal that BMAD is in use in this project.
	info, err := os.Stat(filepath.Join(projectRoot, "_bmad"))
	return err == nil && info.IsDir()
}

func (b *bmadInstaller) Install(projectRoot string, opts Options) (Result, error) {
	path := pathIn(projectRoot, bmadFile)
	res := Result{Path: path}

	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return res, fmt.Errorf("%s not found — run `gg init` first to create it", bmadFile)
		}
		return res, err
	}
	existing := string(raw)
	managed := bmadManagedBody()

	updated, changed, mergeErr := replaceOrAppendBlock(existing, bmadBlockStart, bmadBlockEnd, managed)
	if mergeErr != nil {
		return res, mergeErr
	}
	switch {
	case !changed:
		res.Action = ActionUpToDate
		res.Notes = append(res.Notes, "BMAD relay block already current")
	case opts.DryRun:
		res.Action = ActionDryRun
		res.Notes = append(res.Notes, "would write BMAD relay block to "+bmadFile)
	default:
		if err := os.WriteFile(path, []byte(updated), 0o644); err != nil { //nolint:gosec
			return res, err
		}
		res.Action = ActionUpdated
		res.Notes = append(res.Notes, "BMAD relay block written between "+bmadBlockStart+" / "+bmadBlockEnd)
	}
	return res, nil
}
