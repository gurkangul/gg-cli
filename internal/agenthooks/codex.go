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
	sb.WriteString(codexBlockEnd)
	sb.WriteString("\n")
	return sb.String()
}

type codexInstaller struct{}

func (c *codexInstaller) Name() string { return "codex" }
func (c *codexInstaller) Tier() Tier   { return TierSoft }

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
	res := Result{Path: path}

	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return res, fmt.Errorf("%s not found — run `gg init` first to create it", codexFile)
		}
		return res, err
	}
	existing := string(raw)
	managed := codexManagedBody()

	updated, changed := codexReplaceOrAppendBlock(existing, managed)
	if !changed {
		res.Action = ActionUpToDate
		res.Notes = append(res.Notes, "managed block already current")
		return res, nil
	}

	if opts.DryRun {
		res.Action = ActionDryRun
		res.Notes = append(res.Notes, "would update managed block in "+codexFile)
		return res, nil
	}

	if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
		return res, err
	}
	res.Action = ActionUpdated
	res.Notes = append(res.Notes, "managed block written between "+codexBlockStart+" / "+codexBlockEnd)
	return res, nil
}

// codexReplaceOrAppendBlock returns the updated content and whether any
// change was made. If the managed block markers are already present, the
// region between them (inclusive) is replaced. Otherwise the managed body
// is appended at the end with a blank-line separator.
func codexReplaceOrAppendBlock(content, managed string) (string, bool) {
	startIdx := strings.Index(content, codexBlockStart)
	endIdx := strings.Index(content, codexBlockEnd)

	if startIdx >= 0 && endIdx > startIdx {
		// Replace inclusive of the end marker + its trailing newline (if any).
		blockEnd := endIdx + len(codexBlockEnd)
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

	// Append. Ensure exactly one blank line before the block.
	sep := "\n\n"
	if strings.HasSuffix(content, "\n\n") {
		sep = ""
	} else if strings.HasSuffix(content, "\n") {
		sep = "\n"
	}
	return content + sep + managed, true
}
