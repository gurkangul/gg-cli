package agenthooks

import (
	"os"
)

// SyncResult summarises what SyncManagedBlocks did.
type SyncResult struct {
	// Lines are human-readable one-line entries — one per block that was
	// inspected. Format mirrors FixContract/FixMasterRole/FixDevRouting output
	// so callers can print them verbatim.
	Lines []string
	// Repaired is true when at least one block was written (created or updated).
	Repaired bool
	// Errors collects non-fatal per-block errors. SyncManagedBlocks never
	// returns a hard error — failures are logged here and the sync continues.
	Errors []error
}

// SyncManagedBlocks auto-repairs all managed blocks in projectRoot:
//
//   - Agent-specific blocks (codex managed block, bmad relay, gsd bridge,
//     contract blocks) via InstallDetected — only for agents whose detection
//     signal is already present (Detect returns true).
//
//   - master-role block in CLAUDE.md via FixMasterRole — only when CLAUDE.md
//     already exists (never creates it from scratch here).
//
//   - dev-routing block in CLAUDE.md via FixDevRouting — same guard.
//
// The operation is best-effort and idempotent: a failure for one block is
// logged in SyncResult.Errors and the sync continues. Callers should treat
// SyncResult.Repaired as an "at least one write happened" signal, not as "all
// blocks are now OK".
//
// Intended for session-start auto-repair. Never fatal.
func SyncManagedBlocks(projectRoot string) SyncResult {
	var sr SyncResult

	// Pass 1: all installer-owned blocks (contract + agent-specific managed blocks).
	// InstallDetected runs Detect() so only present agents are touched.
	results := InstallDetected(projectRoot, Options{})
	for _, r := range results {
		switch r.Action {
		case ActionCreated, ActionUpdated:
			sr.Repaired = true
			label := r.DisplayName
			if label == "" {
				label = r.AgentName
			}
			sr.Lines = append(sr.Lines, "  ✓ synced  "+label+" — "+string(r.Action))
		case ActionFailed:
			if r.Err != nil {
				sr.Errors = append(sr.Errors, r.Err)
			}
		}
	}

	// Pass 2: master-role block — only if CLAUDE.md already exists.
	claudeMD := pathIn(projectRoot, "CLAUDE.md")
	if fileExists(claudeMD) {
		lines, err := FixMasterRole(projectRoot, false)
		if err != nil {
			sr.Errors = append(sr.Errors, err)
		} else {
			for _, l := range lines {
				if isRepairLine(l) {
					sr.Repaired = true
					sr.Lines = append(sr.Lines, l)
				}
			}
		}
	}

	// Pass 3: dev-routing block — only if CLAUDE.md already exists.
	if fileExists(claudeMD) {
		lines, err := FixDevRouting(projectRoot, false)
		if err != nil {
			sr.Errors = append(sr.Errors, err)
		} else {
			for _, l := range lines {
				if isRepairLine(l) {
					sr.Repaired = true
					sr.Lines = append(sr.Lines, l)
				}
			}
		}
	}

	return sr
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// isRepairLine returns true for lines that represent an actual write action
// (✓ prefix) rather than a no-op "no action needed" line.
func isRepairLine(l string) bool {
	for _, r := range l {
		if r == ' ' || r == '\t' {
			continue
		}
		return r == '✓'
	}
	return false
}
