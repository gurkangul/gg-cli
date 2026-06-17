package agenthooks

import (
	"os"
	"strings"
)

// Gemini CLI reads GEMINI.md natively from the repo root as its project context
// file. Like Codex it exposes no runtime hook API, so enforcement is strictly
// soft (TierSoft): the agent reads the injected block but may still ignore it.
// We mark our section with stable HTML comments so re-runs replace the block in
// place instead of appending duplicates.
//
// Unlike codex (which errors when AGENTS.md is missing), the gemini installer
// CREATES GEMINI.md when absent: AGENTS.md is the canonical full protocol, and
// GEMINI.md is a thin native pointer to it plus the mandatory orientation block.
const (
	geminiFile       = "GEMINI.md"
	geminiBlockStart = "<!-- gg-managed:start -->"
	geminiBlockEnd   = "<!-- gg-managed:end -->"
)

// geminiHeader is the static lead-in written when GEMINI.md does not exist yet.
// It is outside the managed markers so the user may edit it freely; the managed
// block and contract block are appended/replaced beneath it on every sync.
const geminiHeader = `# Gemini Context

This project uses **gg** as canonical shared memory. The full native-workflow +
durable-memory protocol lives in **AGENTS.md** at the repo root — read it first.

`

// geminiManagedBody returns the full managed block including fence markers.
// Keep this short — the canonical protocol is AGENTS.md, not this section. The
// block exists to make orientation impossible to miss at a stable location, and
// it carries the same MANDATORY orientation wording as the codex block.
func geminiManagedBody() string {
	var sb strings.Builder
	sb.WriteString(geminiBlockStart)
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
	sb.WriteString("Mandatory orientation (run before your first action):\n")
	sb.WriteString("\n")
	sb.WriteString("1. Read the full protocol in AGENTS.md at the repo root.\n")
	sb.WriteString("2. Run `gg session-start --agent <agent_id> --role <role>` as your FIRST action (mandatory when a shell is available).\n")
	sb.WriteString("3. Run `gg search --compact <topic>` before changing important behavior.\n")
	sb.WriteString("4. Record durable decisions/rejections/evidence with `gg record`, `gg task`, `gg bug`, or `gg tell`.\n")
	sb.WriteString("\n")
	sb.WriteString("Skipping orientation means working blind to prior decisions and rejected approaches.\n")
	sb.WriteString("\n")
	sb.WriteString(geminiBlockEnd)
	sb.WriteString("\n")
	return sb.String()
}

type geminiInstaller struct{}

func (g *geminiInstaller) Name() string { return "gemini" }
func (g *geminiInstaller) Tier() Tier   { return TierSoft }
func (g *geminiInstaller) ContractPath(projectRoot string) string {
	return pathIn(projectRoot, geminiFile)
}

func (g *geminiInstaller) Detect(projectRoot string) bool {
	// Mirror codex's reasoning: GEMINI.md is the native context file Gemini CLI
	// reads. We return true whenever GEMINI.md OR a .gemini/ dir already exists,
	// AND we also return true unconditionally so a gg-bootstrapped project always
	// gains the file — the soft block costs nothing when Gemini is not the active
	// agent, but it is there the moment one shows up. (Install creates GEMINI.md
	// when absent, unlike codex which requires AGENTS.md to pre-exist.)
	return true
}

func (g *geminiInstaller) Install(projectRoot string, opts Options) (Result, error) {
	path := pathIn(projectRoot, geminiFile)
	res := Result{Path: path, DisplayName: "GEMINI.md"}

	raw, err := os.ReadFile(path)
	var existing string
	fileExisted := true
	if err != nil {
		if !os.IsNotExist(err) {
			return res, err
		}
		fileExisted = false
		existing = geminiHeader
	} else {
		existing = string(raw)
	}

	managed := geminiManagedBody()
	updated, changed, mergeErr := replaceOrAppendBlock(existing, geminiBlockStart, geminiBlockEnd, managed)
	if mergeErr != nil {
		return res, mergeErr
	}

	switch {
	case !changed && fileExisted:
		res.Action = ActionUpToDate
		res.Notes = append(res.Notes, "managed block already current")
	case opts.DryRun:
		res.Action = ActionDryRun
		if !fileExisted {
			res.Notes = append(res.Notes, "would create "+geminiFile+" with managed block")
		} else {
			res.Notes = append(res.Notes, "would update managed block in "+geminiFile)
		}
	default:
		if err := os.WriteFile(path, []byte(updated), 0o644); err != nil { //nolint:gosec
			return res, err
		}
		if !fileExisted {
			res.Action = ActionCreated
			res.Notes = append(res.Notes, "created "+geminiFile+" pointing to AGENTS.md")
		} else {
			res.Action = ActionUpdated
			res.Notes = append(res.Notes, "managed block written between "+geminiBlockStart+" / "+geminiBlockEnd)
		}
	}

	// Also write the shared contract block to this file so the single-source
	// contract lands in Gemini's native context as well.
	cAction, cNotes, cErr := writeContractBlock(path, opts.DryRun)
	if cErr != nil {
		return res, cErr
	}
	res.Notes = append(res.Notes, cNotes...)
	if res.Action == ActionUpToDate && cAction != ActionUpToDate {
		res.Action = cAction
	}
	return res, nil
}
