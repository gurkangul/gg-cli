package agenthooks

import (
	"crypto/sha256"
	"fmt"
	"os"
	"strings"

	"github.com/gurkangul/gg-cli/internal/templates"
)

const (
	DevRoutingBlockBegin = "<!-- gg:dev-routing:begin v1 -->"
	DevRoutingBlockEnd   = "<!-- gg:dev-routing:end -->"
)

// DevRoutingBody returns the raw dev-routing template body without markers.
func DevRoutingBody() string { return templates.DevRouting }

// DevRoutingBlock returns the full managed dev-routing block including begin/end
// markers. The result is deterministic: byte-for-byte identical across calls.
func DevRoutingBlock() string {
	return DevRoutingBlockBegin + "\n" + templates.DevRouting + DevRoutingBlockEnd + "\n"
}

// DevRoutingVersion returns the hex-encoded SHA256 of the dev-routing body.
// Changes to dev-routing.md yield a different version string — used by
// gg doctor --check-dev-routing to surface drift.
func DevRoutingVersion() string {
	h := sha256.Sum256([]byte(templates.DevRouting))
	return fmt.Sprintf("%x", h)
}

// DevRoutingDriftStatus describes the state of the managed dev-routing block
// in the target file (CLAUDE.md).
type DevRoutingDriftStatus int

const (
	DevRoutingOK       DevRoutingDriftStatus = iota // block present, hash matches current
	DevRoutingSTALE                                 // block present, hash differs
	DevRoutingMISSING                               // markers absent
	DevRoutingDRIFTED                               // one marker present but not the other (malformed)
	DevRoutingEXTENDED                              // both markers present, local body is strict superset of template
)

func (s DevRoutingDriftStatus) String() string {
	switch s {
	case DevRoutingOK:
		return "OK"
	case DevRoutingSTALE:
		return "STALE"
	case DevRoutingMISSING:
		return "MISSING"
	case DevRoutingDRIFTED:
		return "DRIFTED"
	case DevRoutingEXTENDED:
		return "EXTENDED"
	default:
		return "UNKNOWN"
	}
}

// DevRoutingCheckResult is the outcome of one CheckDevRouting call.
type DevRoutingCheckResult struct {
	Path         string
	Status       DevRoutingDriftStatus
	FoundVersion string
	WantVersion  string
}

// CheckDevRouting inspects the dev-routing block in CLAUDE.md at projectRoot
// and returns the drift status.
func CheckDevRouting(projectRoot string) DevRoutingCheckResult {
	want := DevRoutingVersion()
	path := pathIn(projectRoot, "CLAUDE.md")
	r := DevRoutingCheckResult{
		Path:        path,
		WantVersion: want,
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		r.Status = DevRoutingMISSING
		return r
	}

	content := string(raw)
	if hasManagedBlockMarkerDrift(content, DevRoutingBlockBegin, DevRoutingBlockEnd) {
		r.Status = DevRoutingDRIFTED
		return r
	}
	startIdx := strings.Index(content, DevRoutingBlockBegin)
	endIdx := strings.Index(content, DevRoutingBlockEnd)

	switch {
	case startIdx < 0 && endIdx < 0:
		r.Status = DevRoutingMISSING
	case startIdx >= 0 && endIdx > startIdx:
		body := content[startIdx+len(DevRoutingBlockBegin) : endIdx]
		body = strings.TrimPrefix(body, "\n")
		h := sha256.Sum256([]byte(body))
		r.FoundVersion = fmt.Sprintf("%x", h)
		if r.FoundVersion == want {
			r.Status = DevRoutingOK
		} else if isSuperset(body, templates.DevRouting) {
			r.Status = DevRoutingEXTENDED
		} else {
			r.Status = DevRoutingSTALE
		}
	default:
		r.Status = DevRoutingDRIFTED
	}

	return r
}

// FixDevRouting repairs STALE, MISSING, EXTENDED, and DRIFTED entries.
// DRIFTED entries require forceReset=true. EXTENDED entries are always backed
// up to projectRoot/.gg/backups/ before overwriting. Returns per-action
// strings for reporting.
func FixDevRouting(projectRoot string, forceReset bool) ([]string, error) {
	r := CheckDevRouting(projectRoot)
	var lines []string

	switch r.Status {
	case DevRoutingOK:
		lines = append(lines, fmt.Sprintf("  %s  CLAUDE.md — no action needed", r.Status))
	case DevRoutingSTALE, DevRoutingMISSING:
		act, _, err := writeDevRoutingBlock(r.Path, false)
		if err != nil {
			return lines, fmt.Errorf("CLAUDE.md: %w", err)
		}
		lines = append(lines, fmt.Sprintf("  ✓ %s  CLAUDE.md — %s", r.Status, act))
	case DevRoutingEXTENDED:
		backupPath, backupErr := backupFile(projectRoot, r.Path)
		if backupErr != nil {
			return lines, fmt.Errorf("CLAUDE.md: backup failed: %w", backupErr)
		}
		act, _, err := writeDevRoutingBlock(r.Path, false)
		if err != nil {
			return lines, fmt.Errorf("CLAUDE.md: %w", err)
		}
		lines = append(lines, fmt.Sprintf("  ✓ EXTENDED→fixed  CLAUDE.md — %s (local additions backed up to %s)", act, backupPath))
	case DevRoutingDRIFTED:
		if !forceReset {
			lines = append(lines, "  ✗ DRIFTED  CLAUDE.md — markers malformed, rerun with --force-reset to overwrite")
		} else {
			if err := forceStripAndAppend(r.Path, DevRoutingBlockBegin, DevRoutingBlockEnd, DevRoutingBlock()); err != nil {
				return lines, fmt.Errorf("CLAUDE.md: %w", err)
			}
			lines = append(lines, "  ✓ DRIFTED→fixed  CLAUDE.md — malformed markers stripped, block rewritten")
		}
	}
	return lines, nil
}

// writeDevRoutingBlock writes DevRoutingBlock() into the file at path using the
// replace-or-append behaviour. Creates the file if absent.
func writeDevRoutingBlock(path string, dryRun bool) (action Action, notes []string, err error) {
	raw, readErr := os.ReadFile(path)
	var existing string
	fileExisted := true
	if readErr != nil {
		if !os.IsNotExist(readErr) {
			return ActionFailed, nil, readErr
		}
		fileExisted = false
	} else {
		// Strip any legacy version markers before calling replaceOrAppendBlock.
		existing = stripLegacyVersionMarkers(string(raw), DevRoutingBlockBegin, DevRoutingBlockEnd)
	}

	block := DevRoutingBlock()
	updated, changed, mergeErr := replaceOrAppendBlock(existing, DevRoutingBlockBegin, DevRoutingBlockEnd, block)
	if mergeErr != nil {
		return ActionFailed, nil, mergeErr
	}
	if !changed {
		return ActionUpToDate, []string{"dev-routing block already current"}, nil
	}

	if dryRun {
		if !fileExisted {
			return ActionDryRun, []string{"would create " + path + " with dev-routing block"}, nil
		}
		return ActionDryRun, []string{"would update dev-routing block in " + path}, nil
	}

	if err := writeFile(path, updated); err != nil {
		return ActionFailed, nil, err
	}
	if !fileExisted {
		return ActionCreated, []string{"created " + path + " with dev-routing block"}, nil
	}
	return ActionUpdated, []string{"dev-routing block written between " + DevRoutingBlockBegin + " / " + DevRoutingBlockEnd}, nil
}
