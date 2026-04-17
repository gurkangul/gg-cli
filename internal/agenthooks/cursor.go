package agenthooks

import (
	"os"
	"path/filepath"
)

// Cursor rules: .cursor/rules/*.mdc with YAML frontmatter. alwaysApply=true
// means the rule is injected into every prompt the user sends. We write a
// single rule file under a fixed name so re-running is idempotent — we
// just overwrite with the same content, never append.
const cursorRuleFile = "gg-mandatory.mdc"

// cursorRuleContent is the full managed file body. The first frontmatter
// line advertises that this file is managed by gg so users know not to
// hand-edit it — a regenerated file will clobber local changes.
const cursorRuleContent = `---
alwaysApply: true
description: gg-cli enforced protocol (managed by gg — do not edit)
---

# gg-cli Protocol

You are operating inside a gg-cli enforced project. Before acting:

1. Read AGENTS.md at the repo root — it defines this project's protocol.
2. Run ` + "`gg search --compact <topic>`" + ` before proposing anything new.
3. Record every decision/task/rejection with gg — no exceptions.
4. Broadcast substantive work via ` + "`gg tell all --from <role>`" + `.

The rules in AGENTS.md are authoritative. This file is a pointer so the
protocol is visible on every prompt; it is not a substitute for reading
AGENTS.md.
`

type cursorInstaller struct{}

func (c *cursorInstaller) Name() string { return "cursor" }
func (c *cursorInstaller) Tier() Tier   { return TierHard }

func (c *cursorInstaller) Detect(projectRoot string) bool {
	info, err := os.Stat(filepath.Join(projectRoot, ".cursor"))
	return err == nil && info.IsDir()
}

func (c *cursorInstaller) Install(projectRoot string, opts Options) (Result, error) {
	dir := pathIn(projectRoot, ".cursor", "rules")
	path := filepath.Join(dir, cursorRuleFile)
	res := Result{Path: path}

	existing, readErr := os.ReadFile(path)
	if readErr == nil && string(existing) == cursorRuleContent {
		res.Action = ActionUpToDate
		return res, nil
	}

	if opts.DryRun {
		res.Action = ActionDryRun
		if os.IsNotExist(readErr) {
			res.Notes = append(res.Notes, "would create "+path)
		} else {
			res.Notes = append(res.Notes, "would overwrite "+path)
		}
		return res, nil
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return res, err
	}
	if err := os.WriteFile(path, []byte(cursorRuleContent), 0o644); err != nil {
		return res, err
	}
	if os.IsNotExist(readErr) {
		res.Action = ActionCreated
	} else {
		res.Action = ActionUpdated
	}
	res.Notes = append(res.Notes, "alwaysApply rule — injected into every Cursor prompt")
	return res, nil
}
