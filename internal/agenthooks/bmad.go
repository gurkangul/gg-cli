package agenthooks

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// BMAD skill agents (Mary, John, Winston, Amelia, Paige, Sally) run inside
// Claude Code sessions via the BMAD skill framework. They cannot invoke gg
// as a subprocess themselves — the orchestrating agent must extract
// gg-relevant outputs (decisions, tasks, rejections) and call gg directly.
//
// The installer injects a BMAD-specific guidance section into AGENTS.md so
// the orchestrating agent (Claude Code reading AGENTS.md) sees the rule and
// acts as a relay for its BMAD subagents.
//
// Tier: SOFT — the block is readable by the orchestrator but enforcement
// depends on the orchestrator following the instruction.
const (
	bmadFile       = "AGENTS.md"
	bmadBlockStart = "<!-- gg-bmad:start -->"
	bmadBlockEnd   = "<!-- gg-bmad:end -->"
)

func bmadManagedBody() string {
	var sb strings.Builder
	sb.WriteString(bmadBlockStart)
	sb.WriteString("\n")
	sb.WriteString("## BMAD Skill Agents — gg Protocol Relay\n")
	sb.WriteString("\n")
	sb.WriteString("BMAD agents (Mary, John, Winston, Amelia, Paige, Sally, and others) run\n")
	sb.WriteString("inside Claude Code sessions. They cannot call gg directly. As the\n")
	sb.WriteString("orchestrating agent you MUST:\n")
	sb.WriteString("\n")
	sb.WriteString("- After each BMAD round: extract any decisions, task proposals, or\n")
	sb.WriteString("  rejected approaches and persist them with gg immediately.\n")
	sb.WriteString("- Do NOT wait for the user to ask — capture before moving on.\n")
	sb.WriteString("- If a BMAD agent says 'reject X' → `gg record \"X\" --stance=reject --reason \"why\"`\n")
	sb.WriteString("- If a BMAD agent proposes a task → `gg task create \"title\" ...`\n")
	sb.WriteString("- If a BMAD agent reaches a conclusion the user accepts → `gg record \"conclusion\" --reason \"...\"``\n")
	sb.WriteString("\n")
	sb.WriteString(bmadBlockEnd)
	sb.WriteString("\n")
	return sb.String()
}

type bmadInstaller struct{}

func (b *bmadInstaller) Name() string { return "bmad" }
func (b *bmadInstaller) Tier() Tier   { return TierSoft }

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

	updated, changed := bmadReplaceOrAppendBlock(existing, managed)
	if !changed {
		res.Action = ActionUpToDate
		res.Notes = append(res.Notes, "BMAD relay block already current")
		return res, nil
	}

	if opts.DryRun {
		res.Action = ActionDryRun
		res.Notes = append(res.Notes, "would write BMAD relay block to "+bmadFile)
		return res, nil
	}

	if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
		return res, err
	}
	res.Action = ActionUpdated
	res.Notes = append(res.Notes, "BMAD relay block written between "+bmadBlockStart+" / "+bmadBlockEnd)
	return res, nil
}

func bmadReplaceOrAppendBlock(content, managed string) (string, bool) {
	startIdx := strings.Index(content, bmadBlockStart)
	endIdx := strings.Index(content, bmadBlockEnd)

	if startIdx >= 0 && endIdx > startIdx {
		blockEnd := endIdx + len(bmadBlockEnd)
		if blockEnd < len(content) && content[blockEnd] == '\n' {
			blockEnd++
		}
		before := content[:startIdx]
		after := content[blockEnd:]
		newContent := before + managed + after
		if newContent == content {
			return content, false
		}
		return newContent, true
	}

	sep := "\n\n"
	if strings.HasSuffix(content, "\n\n") {
		sep = ""
	} else if strings.HasSuffix(content, "\n") {
		sep = "\n"
	}
	return content + sep + managed, true
}
