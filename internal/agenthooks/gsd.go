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
	sb.WriteString("## gg-cli Bridge\n")
	sb.WriteString("\n")
	sb.WriteString("This project uses gg-cli alongside GSD. Cross-post decisions and task\n")
	sb.WriteString("outcomes to gg so other agents (Claude Code, Cursor, Aider) share the\n")
	sb.WriteString("same memory:\n")
	sb.WriteString("\n")
	sb.WriteString("  gg record \"<decision>\" --reason \"<why>\"   # architectural / pattern decision\n")
	sb.WriteString("  gg task create \"<title>\" --priority high   # delegate a task to another agent\n")
	sb.WriteString("  gg tell \"all\" \"<summary>\" --from gsd       # broadcast milestone progress\n")
	sb.WriteString("\n")
	sb.WriteString("Run `gg status` at slice start to see what other agents have recorded.\n")
	sb.WriteString(gsdMarkerEnd)
	sb.WriteString("\n")
	return sb.String()
}

type gsdInstaller struct{}

func (g *gsdInstaller) Name() string { return "gsd" }
func (g *gsdInstaller) Tier() Tier   { return TierSoft }

func (g *gsdInstaller) Detect(projectRoot string) bool {
	_, err := os.Stat(filepath.Join(projectRoot, gsdDBFile))
	return err == nil
}

func (g *gsdInstaller) Install(projectRoot string, opts Options) (Result, error) {
	path := pathIn(projectRoot, gsdKnowledgeFile)
	res := Result{Path: path}

	raw, err := os.ReadFile(path)
	var existing string
	if err != nil {
		if !os.IsNotExist(err) {
			return res, err
		}
		// KNOWLEDGE.md doesn't exist yet — we'll create it.
		existing = ""
	} else {
		existing = string(raw)
	}

	block := gsdBridgeBlock()

	// Idempotency: if the marker is already present and content matches, skip.
	startIdx := strings.Index(existing, gsdMarker)
	endIdx := strings.Index(existing, gsdMarkerEnd)
	if startIdx >= 0 && endIdx > startIdx {
		endPos := endIdx + len(gsdMarkerEnd)
		if endPos < len(existing) && existing[endPos] == '\n' {
			endPos++
		}
		current := existing[startIdx:endPos]
		if strings.TrimSpace(current) == strings.TrimSpace(block) {
			res.Action = ActionUpToDate
			res.Notes = append(res.Notes, "gg-bridge block already current")
			return res, nil
		}
		// Replace drifted block.
		before := existing[:startIdx]
		after := existing[endPos:]
		updated := before + block + after
		if opts.DryRun {
			res.Action = ActionDryRun
			res.Notes = append(res.Notes, "would update gg-bridge block in "+gsdKnowledgeFile)
			return res, nil
		}
		if err := writeFile(path, updated); err != nil {
			return res, err
		}
		res.Action = ActionUpdated
		res.Notes = append(res.Notes, "gg-bridge block updated in "+gsdKnowledgeFile)
		return res, nil
	}

	// Append block.
	sep := "\n\n"
	if strings.HasSuffix(existing, "\n\n") {
		sep = ""
	} else if strings.HasSuffix(existing, "\n") {
		sep = "\n"
	}
	updated := existing + sep + block

	if opts.DryRun {
		res.Action = ActionDryRun
		res.Notes = append(res.Notes, "would write gg-bridge block to "+gsdKnowledgeFile)
		return res, nil
	}

	if err := writeFile(path, updated); err != nil {
		return res, err
	}
	if existing == "" {
		res.Action = ActionCreated
		res.Notes = append(res.Notes, "created "+gsdKnowledgeFile+" with gg-bridge block")
	} else {
		res.Action = ActionUpdated
		res.Notes = append(res.Notes, "appended gg-bridge block to "+gsdKnowledgeFile)
	}
	return res, nil
}

func writeFile(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644) //nolint:gosec
}
