package agenthooks

import (
	"crypto/sha256"
	"fmt"
	"os"
	"strings"

	"github.com/gurkangul/gg-cli/internal/templates"
)

const (
	MasterRoleBlockBegin = "<!-- gg:master-role:begin v1 -->"
	MasterRoleBlockEnd   = "<!-- gg:master-role:end -->"
)

// MasterRoleBody returns the raw master-role template body without markers.
func MasterRoleBody() string { return templates.MasterRole }

// MasterRoleBlock returns the full managed master-role block including begin/end
// markers. The result is deterministic: byte-for-byte identical across calls.
func MasterRoleBlock() string {
	return MasterRoleBlockBegin + "\n" + templates.MasterRole + MasterRoleBlockEnd + "\n"
}

// MasterRoleVersion returns the hex-encoded SHA256 of the master-role body.
// Changes to master-role.md yield a different version string — used by
// gg doctor --check-master-role to surface drift.
func MasterRoleVersion() string {
	h := sha256.Sum256([]byte(templates.MasterRole))
	return fmt.Sprintf("%x", h)
}

// MasterRoleDriftStatus describes the state of the managed master-role block
// in the target file (CLAUDE.md).
type MasterRoleDriftStatus int

const (
	MasterRoleOK       MasterRoleDriftStatus = iota // block present, hash matches current
	MasterRoleSTALE                                 // block present, hash differs
	MasterRoleMISSING                               // markers absent
	MasterRoleDRIFTED                               // one marker present but not the other (malformed)
	MasterRoleEXTENDED                              // both markers present, local body is strict superset of template
)

func (s MasterRoleDriftStatus) String() string {
	switch s {
	case MasterRoleOK:
		return "OK"
	case MasterRoleSTALE:
		return "STALE"
	case MasterRoleMISSING:
		return "MISSING"
	case MasterRoleDRIFTED:
		return "DRIFTED"
	case MasterRoleEXTENDED:
		return "EXTENDED"
	default:
		return "UNKNOWN"
	}
}

// MasterRoleCheckResult is the outcome of one CheckMasterRole call.
type MasterRoleCheckResult struct {
	Path         string
	Status       MasterRoleDriftStatus
	FoundVersion string
	WantVersion  string
}

// CheckMasterRole inspects the master-role block in CLAUDE.md at projectRoot
// and returns the drift status.
func CheckMasterRole(projectRoot string) MasterRoleCheckResult {
	want := MasterRoleVersion()
	path := pathIn(projectRoot, "CLAUDE.md")
	r := MasterRoleCheckResult{
		Path:        path,
		WantVersion: want,
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		r.Status = MasterRoleMISSING
		return r
	}

	content := string(raw)
	startIdx := strings.Index(content, MasterRoleBlockBegin)
	endIdx := strings.Index(content, MasterRoleBlockEnd)

	switch {
	case startIdx < 0 && endIdx < 0:
		r.Status = MasterRoleMISSING
	case startIdx >= 0 && endIdx > startIdx:
		body := content[startIdx+len(MasterRoleBlockBegin) : endIdx]
		body = strings.TrimPrefix(body, "\n")
		h := sha256.Sum256([]byte(body))
		r.FoundVersion = fmt.Sprintf("%x", h)
		if r.FoundVersion == want {
			r.Status = MasterRoleOK
		} else if isSuperset(body, templates.MasterRole) {
			r.Status = MasterRoleEXTENDED
		} else {
			r.Status = MasterRoleSTALE
		}
	default:
		r.Status = MasterRoleDRIFTED
	}

	return r
}

// FixMasterRole repairs STALE, MISSING, EXTENDED, and DRIFTED entries.
// DRIFTED entries require forceReset=true. EXTENDED entries are always backed
// up to projectRoot/.gg/backups/ before overwriting. Returns per-action
// strings for reporting.
func FixMasterRole(projectRoot string, forceReset bool) ([]string, error) {
	r := CheckMasterRole(projectRoot)
	var lines []string

	switch r.Status {
	case MasterRoleOK:
		lines = append(lines, fmt.Sprintf("  %s  CLAUDE.md — no action needed", r.Status))
	case MasterRoleSTALE, MasterRoleMISSING:
		act, _, err := writeMasterRoleBlock(r.Path, false)
		if err != nil {
			return lines, fmt.Errorf("CLAUDE.md: %w", err)
		}
		lines = append(lines, fmt.Sprintf("  ✓ %s  CLAUDE.md — %s", r.Status, act))
	case MasterRoleEXTENDED:
		backupPath, backupErr := backupFile(projectRoot, r.Path)
		if backupErr != nil {
			return lines, fmt.Errorf("CLAUDE.md: backup failed: %w", backupErr)
		}
		act, _, err := writeMasterRoleBlock(r.Path, false)
		if err != nil {
			return lines, fmt.Errorf("CLAUDE.md: %w", err)
		}
		lines = append(lines, fmt.Sprintf("  ✓ EXTENDED→fixed  CLAUDE.md — %s (local additions backed up to %s)", act, backupPath))
	case MasterRoleDRIFTED:
		if !forceReset {
			lines = append(lines, "  ✗ DRIFTED  CLAUDE.md — markers malformed, rerun with --force-reset to overwrite")
		} else {
			if err := forceStripAndAppend(r.Path, MasterRoleBlockBegin, MasterRoleBlockEnd, MasterRoleBlock()); err != nil {
				return lines, fmt.Errorf("CLAUDE.md: %w", err)
			}
			lines = append(lines, "  ✓ DRIFTED→fixed  CLAUDE.md — malformed markers stripped, block rewritten")
		}
	}
	return lines, nil
}

// writeMasterRoleBlock writes MasterRoleBlock() into the file at path using the
// replace-or-append behaviour. Creates the file if absent.
func writeMasterRoleBlock(path string, dryRun bool) (action Action, notes []string, err error) {
	raw, readErr := os.ReadFile(path)
	var existing string
	fileExisted := true
	if readErr != nil {
		if !os.IsNotExist(readErr) {
			return ActionFailed, nil, readErr
		}
		fileExisted = false
	} else {
		existing = string(raw)
	}

	block := MasterRoleBlock()
	updated, changed, mergeErr := replaceOrAppendBlock(existing, MasterRoleBlockBegin, MasterRoleBlockEnd, block)
	if mergeErr != nil {
		return ActionFailed, nil, mergeErr
	}
	if !changed {
		return ActionUpToDate, []string{"master-role block already current"}, nil
	}

	if dryRun {
		if !fileExisted {
			return ActionDryRun, []string{"would create " + path + " with master-role block"}, nil
		}
		return ActionDryRun, []string{"would update master-role block in " + path}, nil
	}

	if err := writeFile(path, updated); err != nil {
		return ActionFailed, nil, err
	}
	if !fileExisted {
		return ActionCreated, []string{"created " + path + " with master-role block"}, nil
	}
	return ActionUpdated, []string{"master-role block written between " + MasterRoleBlockBegin + " / " + MasterRoleBlockEnd}, nil
}
