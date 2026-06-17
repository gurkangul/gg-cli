package agenthooks

import (
	"os"
	"strings"
)

// OpenHands reads repo-root AGENTS.md natively (modern builds) AND repo-local
// microagents under .openhands/microagents/*.md. We write a single "always"
// microagent so the gg orientation is injected into every OpenHands prompt for
// this repo. There is no runtime gate we can enforce, so this is TierSoft: the
// agent reads the block but may still ignore it.
//
// The microagent body is wrapped in stable HTML-comment markers so re-runs
// replace the block in place (idempotent) instead of appending duplicates. The
// frontmatter sits outside the markers and is rewritten only when the file is
// created fresh.
const (
	openhandsFile       = ".openhands/microagents/gg.md"
	openhandsBlockStart = "<!-- gg-managed:start -->"
	openhandsBlockEnd   = "<!-- gg-managed:end -->"
)

// openhandsFrontmatter is the minimal valid OpenHands microagent header. An
// "always"-loaded knowledge microagent needs name + a body; declaring it always
// makes it part of the system prompt for every conversation in this repo.
const openhandsFrontmatter = `---
name: gg
type: knowledge
always: true
---

# gg Durable Memory (OpenHands microagent — managed by gg)

The full native-workflow + durable-memory protocol lives in **AGENTS.md** at the
repo root. Read it first.

`

// openhandsManagedBody returns the full managed block including fence markers.
// Same MANDATORY orientation wording as the codex/gemini blocks; the canonical
// protocol is AGENTS.md, this block just makes orientation impossible to miss.
func openhandsManagedBody() string {
	var sb strings.Builder
	sb.WriteString(openhandsBlockStart)
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
	sb.WriteString(openhandsBlockEnd)
	sb.WriteString("\n")
	return sb.String()
}

type openhandsInstaller struct{}

func (o *openhandsInstaller) Name() string { return "openhands" }
func (o *openhandsInstaller) Tier() Tier   { return TierSoft }
func (o *openhandsInstaller) ContractPath(projectRoot string) string {
	return pathIn(projectRoot, ".openhands", "microagents", "gg.md")
}

func (o *openhandsInstaller) Detect(projectRoot string) bool {
	// Mirror codex/gemini reasoning: return true unconditionally so any
	// gg-bootstrapped project gains the microagent. The file costs nothing when
	// OpenHands is not the active agent (other runtimes never read
	// .openhands/microagents/), but it is in place the moment OpenHands shows up.
	// Install creates the file + parent dirs when absent.
	return true
}

func (o *openhandsInstaller) Install(projectRoot string, opts Options) (Result, error) {
	path := pathIn(projectRoot, ".openhands", "microagents", "gg.md")
	res := Result{Path: path, DisplayName: ".openhands/microagents/gg.md"}

	raw, err := os.ReadFile(path)
	var existing string
	fileExisted := true
	if err != nil {
		if !os.IsNotExist(err) {
			return res, err
		}
		fileExisted = false
		existing = openhandsFrontmatter
	} else {
		existing = string(raw)
	}

	managed := openhandsManagedBody()
	updated, changed, mergeErr := replaceOrAppendBlock(existing, openhandsBlockStart, openhandsBlockEnd, managed)
	if mergeErr != nil {
		return res, mergeErr
	}

	switch {
	case !changed && fileExisted:
		res.Action = ActionUpToDate
		res.Notes = append(res.Notes, "managed block already current")
		return res, nil
	case opts.DryRun:
		res.Action = ActionDryRun
		if !fileExisted {
			res.Notes = append(res.Notes, "would create "+openhandsFile+" with managed block")
		} else {
			res.Notes = append(res.Notes, "would update managed block in "+openhandsFile)
		}
		return res, nil
	default:
		// writeFile creates the .openhands/microagents/ dir if needed.
		if err := writeFile(path, updated); err != nil {
			return res, err
		}
		if !fileExisted {
			res.Action = ActionCreated
			res.Notes = append(res.Notes, "created "+openhandsFile+" pointing to AGENTS.md")
		} else {
			res.Action = ActionUpdated
			res.Notes = append(res.Notes, "managed block written between "+openhandsBlockStart+" / "+openhandsBlockEnd)
		}
		return res, nil
	}
}
