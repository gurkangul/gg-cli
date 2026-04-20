package agenthooks

import (
	"crypto/sha256"
	"fmt"
	"os"
	"strings"

	"github.com/gurkangul/gg-cli/internal/templates"
)

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
