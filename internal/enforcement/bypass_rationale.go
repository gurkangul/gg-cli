package enforcement

import (
	"fmt"
	"os"
	"strings"
)

// BypassRationaleEnvVar is the env var that must be set to a non-empty value
// when GG_ENFORCEMENT=off. Format: "TASK-NNN: reason" or just "reason".
// When a task-scoped gate is bypassed (taskID non-empty), the TASK-NNN prefix
// in the rationale is validated to match that task — a rationale written for a
// different task is rejected so cross-task rationale recycling is caught.
const BypassRationaleEnvVar = "GG_BYPASS_RATIONALE"

// BypassRationaleResult is the outcome of CheckBypassRationale.
type BypassRationaleResult struct {
	// Rationale is the trimmed value of GG_BYPASS_RATIONALE.
	Rationale string
	// RationaleTaskID is the TASK-NNN parsed from the rationale (e.g. "TASK-317").
	// Empty when the rationale has no TASK prefix or when taskID is empty.
	RationaleTaskID string
}

// CheckBypassRationale validates that GG_BYPASS_RATIONALE is set and, for
// task-scoped gates, that its TASK-NNN prefix matches taskID.
//
// Returns an error when:
//   - GG_BYPASS_RATIONALE is empty → silent bypass rejected
//   - taskID is non-empty AND the rationale carries a TASK-NNN prefix that
//     does not match taskID → cross-task rationale recycling rejected
//
// Returns the parsed result on success.
func CheckBypassRationale(taskID string) (BypassRationaleResult, error) {
	raw := strings.TrimSpace(os.Getenv(BypassRationaleEnvVar))
	if raw == "" {
		msg := fmt.Sprintf(
			"silent bypass rejected: GG_ENFORCEMENT=off requires %s=\"<reason>\".\n"+
				"Provide a rationale so the bypass is auditable:\n\n"+
				"  %s=\"%s: <why this bypass is necessary>\" gg task done %s ...\n\n"+
				"This is TASK-317 enforcement — silent master bypasses are forbidden.\n"+
				"See CLAUDE.md § Master bypass discipline for the full policy.",
			BypassRationaleEnvVar, BypassRationaleEnvVar, taskID, taskID)
		return BypassRationaleResult{}, fmt.Errorf("%s", msg)
	}

	rationaleTaskID := extractTaskIDFromRationale(raw)

	// When the gate is task-scoped AND the rationale has a TASK prefix,
	// the prefix must match the task being closed. This catches the pattern
	// where an agent re-uses an old rationale from a different task.
	if taskID != "" && rationaleTaskID != "" && !strings.EqualFold(rationaleTaskID, taskID) {
		return BypassRationaleResult{}, fmt.Errorf(
			"bypass rationale task mismatch: %s=%q references %s but gate is for %s.\n"+
				"Update the rationale to reference the correct task:\n\n"+
				"  %s=\"%s: <why>\" gg task done %s ...",
			BypassRationaleEnvVar, raw, rationaleTaskID, taskID,
			BypassRationaleEnvVar, taskID, taskID)
	}

	return BypassRationaleResult{Rationale: raw, RationaleTaskID: rationaleTaskID}, nil
}

// extractTaskIDFromRationale parses the optional "TASK-NNN:" or "TASK-NNN "
// prefix from a rationale string. Returns "" when no TASK prefix is found.
// Matching is case-insensitive so "task-317" and "TASK-317" both work.
func extractTaskIDFromRationale(rationale string) string {
	upper := strings.ToUpper(rationale)
	if !strings.HasPrefix(upper, "TASK-") {
		return ""
	}
	// Slice off "TASK-" and read digits until a non-digit/non-dash stop char.
	rest := rationale[5:]
	end := 0
	for end < len(rest) && (rest[end] >= '0' && rest[end] <= '9') {
		end++
	}
	if end == 0 {
		return ""
	}
	return "TASK-" + rest[:end]
}
