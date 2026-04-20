package agenthooks

import (
	"os"
	"path/filepath"
	"strings"
)

// GSD (Get Shit Done) is a separate MCP server backed by .gsd/gsd.db. GSD
// agents run in pi (the GSD harness) and have their own planning primitives.
// There is no GSD hook API we can write to that is guaranteed to persist
// across sessions without forking GSD itself.
//
// The installer uses TierSoft and writes a gg-bridge advisory to
// .gsd/KNOWLEDGE.md — a file GSD agents always read at the start of each
// unit. This makes the gg cross-posting rule visible without touching GSD
// internals.
//
// If KNOWLEDGE.md already exists the installer appends idempotently behind
// a stable marker line. If it doesn't exist yet it creates a minimal file.

const (
	gsdDBFile        = ".gsd/gsd.db"
	gsdKnowledgeFile = ".gsd/KNOWLEDGE.md"
	gsdMarker        = "<!-- gg-bridge -->"
	gsdMarkerEnd     = "<!-- /gg-bridge -->"
)

func gsdBridgeBlock() string {
	var sb strings.Builder
	sb.WriteString(gsdMarker)
	sb.WriteString("\n")
	sb.WriteString("## gg-cli Bridge — MANDATORY PER-TASK MIRROR\n")
	sb.WriteString("\n")
	sb.WriteString("This project uses gg-cli alongside GSD. **Other agents (Claude Code,\n")
	sb.WriteString("Cursor, Aider) cannot read `.gsd/gsd.db`.** Work that lives only in GSD\n")
	sb.WriteString("is invisible to them. gg is the canonical cross-agent store; GSD is a\n")
	sb.WriteString("planning workspace that must cross-post.\n")
	sb.WriteString("\n")
	sb.WriteString("### The rule\n")
	sb.WriteString("\n")
	sb.WriteString("**Every GSD task (T-level) MUST have exactly one gg task mirror.** Not\n")
	sb.WriteString("milestones, not slices — per-T-level-task. Slice-summary mirrors are a\n")
	sb.WriteString("drift anti-pattern that hides in-flight work from the rest of the team.\n")
	sb.WriteString("\n")
	sb.WriteString("### Lifecycle — do this, not a slice summary\n")
	sb.WriteString("\n")
	sb.WriteString("At pickup (when you `gsd_execute` a task T0N):\n")
	sb.WriteString("\n")
	sb.WriteString("    gg task create \"[GSD:M001-S02-T05] short title\" \\\n")
	sb.WriteString("      --detail \"<1-line scope from GSD task.detail>\" \\\n")
	sb.WriteString("      --priority <gsd priority> --tags \"gsd,slice-02\"\n")
	sb.WriteString("\n")
	sb.WriteString("At completion (after `gsd_complete_task`):\n")
	sb.WriteString("\n")
	sb.WriteString("    gg tell \"all\" \"TASK-NNN done — <one-line outcome>\" --from gsd\n")
	sb.WriteString("    # Never self-close your own mirror. Reviewer (the user or another\n")
	sb.WriteString("    # agent) runs `gg task done TASK-NNN` after verifying the AC.\n")
	sb.WriteString("\n")
	sb.WriteString("For a pure architectural decision (no task):\n")
	sb.WriteString("\n")
	sb.WriteString("    gg record \"<decision>\" --reason \"<why>\" --tags \"gsd,architecture\"\n")
	sb.WriteString("\n")
	sb.WriteString("### Session hygiene\n")
	sb.WriteString("\n")
	sb.WriteString("- `gg status` at slice start — see what other agents recorded.\n")
	sb.WriteString("- If GSD planning created N tasks and gg shows fewer than N, mirrors are\n")
	sb.WriteString("  missing — fix before proceeding. A partial mirror is worse than none.\n")
	sb.WriteString("- `GG_AGENT=gsd` export tags every gg call so telemetry splits pure-GSD\n")
	sb.WriteString("  sessions from hybrid Claude/GSD work.\n")
	sb.WriteString(gsdMarkerEnd)
	sb.WriteString("\n")
	return sb.String()
}

type gsdInstaller struct{}

func (g *gsdInstaller) Name() string { return "gsd" }
func (g *gsdInstaller) Tier() Tier   { return TierSoft }
func (g *gsdInstaller) ContractPath(projectRoot string) string {
	return pathIn(projectRoot, gsdKnowledgeFile)
}

func (g *gsdInstaller) Detect(projectRoot string) bool {
	_, err := os.Stat(filepath.Join(projectRoot, gsdDBFile))
	return err == nil
}

func (g *gsdInstaller) Install(projectRoot string, opts Options) (Result, error) {
	path := pathIn(projectRoot, gsdKnowledgeFile)
	res := Result{Path: path}

	raw, err := os.ReadFile(path)
	var existing string
	fileExisted := true
	if err != nil {
		if !os.IsNotExist(err) {
			return res, err
		}
		fileExisted = false
	} else {
		existing = string(raw)
	}

	block := gsdBridgeBlock()
	updated, changed, mergeErr := replaceOrAppendBlock(existing, gsdMarker, gsdMarkerEnd, block)
	if mergeErr != nil {
		return res, mergeErr
	}
	if !changed {
		res.Action = ActionUpToDate
		res.Notes = append(res.Notes, "gg-bridge block already current")
	} else if opts.DryRun {
		res.Action = ActionDryRun
		if !fileExisted {
			res.Notes = append(res.Notes, "would write gg-bridge block to "+gsdKnowledgeFile)
		} else {
			res.Notes = append(res.Notes, "would update gg-bridge block in "+gsdKnowledgeFile)
		}
	} else {
		if err := writeFile(path, updated); err != nil {
			return res, err
		}
		if !fileExisted {
			res.Action = ActionCreated
			res.Notes = append(res.Notes, "created "+gsdKnowledgeFile+" with gg-bridge block")
		} else {
			res.Action = ActionUpdated
			res.Notes = append(res.Notes, "gg-bridge block updated in "+gsdKnowledgeFile)
		}
	}

	// Also write the shared contract block to this file.
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

func writeFile(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644) //nolint:gosec
}
