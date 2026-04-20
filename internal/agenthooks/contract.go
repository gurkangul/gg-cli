package agenthooks

import (
	"crypto/sha256"
	"fmt"
	"os"
	"strings"

	"github.com/gurkangul/gg-cli/internal/templates"
)

// ContractDriftStatus describes the state of the managed contract block in one
// agent's entry-point file.
type ContractDriftStatus int

const (
	ContractOK      ContractDriftStatus = iota // block present, hash matches current
	ContractSTALE                              // block present, hash differs
	ContractMISSING                            // markers absent
	ContractDRIFTED                            // one marker present but not the other (malformed)
)

func (s ContractDriftStatus) String() string {
	switch s {
	case ContractOK:
		return "OK"
	case ContractSTALE:
		return "STALE"
	case ContractMISSING:
		return "MISSING"
	case ContractDRIFTED:
		return "DRIFTED"
	default:
		return "UNKNOWN"
	}
}

// ContractCheckResult is the outcome for one agent.
type ContractCheckResult struct {
	AgentName string
	Path      string
	Status    ContractDriftStatus
	// FoundVersion is the SHA256 of the block body found on disk (empty for MISSING/DRIFTED).
	FoundVersion string
	// WantVersion is ContractVersion() — the current expected hash.
	WantVersion string
}

// CheckContract inspects the contract block in every registered agent's
// entry-point file and returns a result per agent. Only agents whose contract
// file already exists on disk are checked; agents with no file get MISSING.
func CheckContract(projectRoot string) []ContractCheckResult {
	want := ContractVersion()
	results := make([]ContractCheckResult, 0, len(registry))
	seen := make(map[string]bool)

	for _, inst := range registry {
		path := inst.ContractPath(projectRoot)
		// Deduplicate paths — codex and bmad both target AGENTS.md.
		if seen[path] {
			continue
		}
		seen[path] = true

		r := ContractCheckResult{
			AgentName:   inst.Name(),
			Path:        path,
			WantVersion: want,
		}

		raw, err := os.ReadFile(path)
		if err != nil {
			r.Status = ContractMISSING
			results = append(results, r)
			continue
		}

		content := string(raw)
		startIdx := strings.Index(content, ContractBlockBegin)
		endIdx := strings.Index(content, ContractBlockEnd)

		switch {
		case startIdx < 0 && endIdx < 0:
			r.Status = ContractMISSING
		case startIdx >= 0 && endIdx > startIdx:
			body := content[startIdx+len(ContractBlockBegin) : endIdx]
			if strings.HasPrefix(body, "\n") {
				body = body[1:]
			}
			h := sha256.Sum256([]byte(body))
			r.FoundVersion = fmt.Sprintf("%x", h)
			if r.FoundVersion == want {
				r.Status = ContractOK
			} else {
				r.Status = ContractSTALE
			}
		default:
			r.Status = ContractDRIFTED
		}

		results = append(results, r)
	}
	return results
}

// FixContract repairs STALE and MISSING entries. DRIFTED entries are skipped
// unless forceReset is true. Returns per-agent action strings for reporting.
func FixContract(projectRoot string, forceReset bool) ([]string, error) {
	checks := CheckContract(projectRoot)
	var lines []string

	for _, r := range checks {
		switch r.Status {
		case ContractOK:
			lines = append(lines, fmt.Sprintf("  %s  %s — no action needed", r.Status, r.AgentName))
		case ContractSTALE, ContractMISSING:
			act, _, err := writeContractBlock(r.Path, false)
			if err != nil {
				return lines, fmt.Errorf("%s: %w", r.AgentName, err)
			}
			lines = append(lines, fmt.Sprintf("  ✓ %s  %s — %s", r.Status, r.AgentName, act))
		case ContractDRIFTED:
			if !forceReset {
				lines = append(lines, fmt.Sprintf("  ✗ DRIFTED  %s — markers malformed, rerun with --force-reset to overwrite", r.AgentName))
			} else {
				act, _, err := writeContractBlock(r.Path, false)
				if err != nil {
					return lines, fmt.Errorf("%s: %w", r.AgentName, err)
				}
				lines = append(lines, fmt.Sprintf("  ✓ DRIFTED→fixed  %s — %s", r.AgentName, act))
			}
		}
	}
	return lines, nil
}

// HasDrift returns true if any registered agent has a STALE or MISSING contract.
// Used by session-start to emit a one-line warning without blocking startup.
func HasDrift(projectRoot string) bool {
	for _, r := range CheckContract(projectRoot) {
		if r.Status == ContractSTALE || r.Status == ContractMISSING {
			return true
		}
	}
	return false
}

const (
	ContractBlockBegin = "<!-- gg:contract:begin v1 -->"
	ContractBlockEnd   = "<!-- gg:contract:end -->"
)

// ContractBody returns the raw contract template body without markers.
func ContractBody() string { return templates.AgentContract }

// ContractBlock returns the full managed contract block including begin/end
// markers. The result is deterministic: byte-for-byte identical across calls.
func ContractBlock() string {
	return ContractBlockBegin + "\n" + templates.AgentContract + ContractBlockEnd + "\n"
}

// ContractVersion returns the hex-encoded SHA256 of the contract body.
// Changes to agent-contract.md yield a different version string — used by
// gg doctor --check-contract (TASK-255) to surface drift.
func ContractVersion() string {
	h := sha256.Sum256([]byte(templates.AgentContract))
	return fmt.Sprintf("%x", h)
}

// replaceOrAppendBlock inserts managed (a complete block including its own
// markers) into content. Rules:
//   - Both begin and end present: replace the region between them.
//   - Only one marker present: error (malformed — user must fix manually).
//   - Neither present: append at EOF with a blank-line separator.
//
// The second return value reports whether content actually changed.
func replaceOrAppendBlock(content, begin, end, managed string) (string, bool, error) {
	startIdx := strings.Index(content, begin)
	endIdx := strings.Index(content, end)

	switch {
	case startIdx >= 0 && endIdx > startIdx:
		blockEnd := endIdx + len(end)
		if blockEnd < len(content) && content[blockEnd] == '\n' {
			blockEnd++
		}
		before := content[:startIdx]
		after := content[blockEnd:]
		newContent := before + managed + after
		if newContent == content {
			return content, false, nil
		}
		return newContent, true, nil
	case startIdx >= 0 && endIdx < 0:
		return "", false, fmt.Errorf("malformed contract markers: %q present but %q missing — fix manually", begin, end)
	case startIdx < 0 && endIdx >= 0:
		return "", false, fmt.Errorf("malformed contract markers: %q present but %q missing — fix manually", end, begin)
	default:
		sep := "\n\n"
		if strings.HasSuffix(content, "\n\n") {
			sep = ""
		} else if strings.HasSuffix(content, "\n") {
			sep = "\n"
		}
		return content + sep + managed, true, nil
	}
}

// writeContractBlock writes ContractBlock() into the file at path using the
// replace-or-append behaviour. Creates the file if absent. Returns an error
// (without touching the file) if the marker pair is malformed.
func writeContractBlock(path string, dryRun bool) (action Action, notes []string, err error) {
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

	block := ContractBlock()
	updated, changed, mergeErr := replaceOrAppendBlock(existing, ContractBlockBegin, ContractBlockEnd, block)
	if mergeErr != nil {
		return ActionFailed, nil, mergeErr
	}
	if !changed {
		return ActionUpToDate, []string{"contract block already current"}, nil
	}

	if dryRun {
		if !fileExisted {
			return ActionDryRun, []string{"would create " + path + " with contract block"}, nil
		}
		return ActionDryRun, []string{"would update contract block in " + path}, nil
	}

	if err := writeFile(path, updated); err != nil {
		return ActionFailed, nil, err
	}
	if !fileExisted {
		return ActionCreated, []string{"created " + path + " with contract block"}, nil
	}
	return ActionUpdated, []string{"contract block written between " + ContractBlockBegin + " / " + ContractBlockEnd}, nil
}
