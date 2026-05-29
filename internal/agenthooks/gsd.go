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
	sb.WriteString("## gg-cli Bridge — Native GSD Scratchpad\n")
	sb.WriteString("\n")
	sb.WriteString("gg-cli does not own GSD's workflow. GSD may be used as a native planning or\n")
	sb.WriteString("execution scratchpad/helper. The shared-memory rule is: durable outcomes that\n")
	sb.WriteString("future agents need must be copied into gg.\n")
	sb.WriteString("\n")
	sb.WriteString("This project uses gg-cli alongside GSD. Other agents (Claude Code, Cursor,\n")
	sb.WriteString("Aider) cannot read `.gsd/gsd.db`. Work that lives only in GSD is invisible to\n")
	sb.WriteString("them. gg is the canonical cross-agent memory; GSD is a local workspace for\n")
	sb.WriteString("exploration, specs, context, and task planning.\n")
	sb.WriteString("\n")
	sb.WriteString("### The rule\n")
	sb.WriteString("\n")
	sb.WriteString("Do not treat `.gsd/gsd.db` as shared memory. Create a gg task only for durable\n")
	sb.WriteString("work that matters to the project, record decisions with `gg record`, record\n")
	sb.WriteString("bugs/root causes with `gg bug`, and send cross-agent handoffs with `gg tell`.\n")
	sb.WriteString("\n")
	sb.WriteString("### Useful commands\n")
	sb.WriteString("\n")
	sb.WriteString("For durable work:\n")
	sb.WriteString("\n")
	sb.WriteString("    gg task create \"<short title>\" --detail \"<scope>\" --priority medium --tags \"gsd\"\n")
	sb.WriteString("\n")
	sb.WriteString("For useful progress, blockers, handoffs, or evidence summaries:\n")
	sb.WriteString("\n")
	sb.WriteString("    gg tell \"all\" \"<one-line outcome; evidence: commands=<cmds>; live=<smoke>; impact=<files>; gaps=<none|gap>; artifacts=<paths>>\" --from gsd --audience agents\n")
	sb.WriteString("\n")
	sb.WriteString("For decisions or rejected approaches:\n")
	sb.WriteString("\n")
	sb.WriteString("    gg record \"<decision>\" --reason \"<why>\" --tags \"gsd,architecture\"\n")
	sb.WriteString("    gg record \"<rejected approach>\" --decision-status rejected --reason \"<why not>\" --tags \"gsd\"\n")
	sb.WriteString("\n")
	sb.WriteString("### Session hygiene\n")
	sb.WriteString("\n")
	sb.WriteString("- `gg status` or `gg context --compact` shows what other agents recorded.\n")
	sb.WriteString("- `gg gsd audit` is advisory: it can reveal GSD tasks that may need a durable\n")
	sb.WriteString("  gg task, but scratchpad-only items are not failures by themselves.\n")
	sb.WriteString("- Set `GG_AGENT` to the runtime's own identity (for a manual GSD terminal,\n")
	sb.WriteString("  usually `gsd`) so telemetry can distinguish agent-initiated calls.\n")
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
	switch {
	case !changed:
		res.Action = ActionUpToDate
		res.Notes = append(res.Notes, "gg-bridge block already current")
	case opts.DryRun:
		res.Action = ActionDryRun
		if !fileExisted {
			res.Notes = append(res.Notes, "would write gg-bridge block to "+gsdKnowledgeFile)
		} else {
			res.Notes = append(res.Notes, "would update gg-bridge block in "+gsdKnowledgeFile)
		}
	default:
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
