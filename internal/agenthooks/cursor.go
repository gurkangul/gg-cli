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

// cursorRuleFrontmatter is the YAML frontmatter prepended to the rule file.
// It is not inside the contract markers so it can be updated independently.
const cursorRuleFrontmatter = `---
alwaysApply: true
description: gg-cli enforced protocol (managed by gg — do not edit)
---

You are operating inside a gg-cli enforced project. The rules below are
mandatory. Read AGENTS.md at the repo root for the full protocol.

`

// cursorRuleContent constructs the full managed file body: static frontmatter
// followed by the single-source contract block.
func cursorRuleContent() string {
	return cursorRuleFrontmatter + ContractBlock()
}

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

	want := cursorRuleContent()
	existing, readErr := os.ReadFile(path)
	if readErr == nil && string(existing) == want {
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
	if err := os.WriteFile(path, []byte(want), 0o644); err != nil {
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
