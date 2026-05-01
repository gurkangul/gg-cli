package enforcement

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

// validRecordIDRe matches valid gg record IDs:
//   - UUID v4 (8-4-4-4-12 hex groups, case-insensitive)
//   - Display IDs like DEC-001, DEC-42, etc. (case-insensitive)
var validRecordIDRe = regexp.MustCompile(
	`(?i)^` +
		`([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}` + // UUID
		`|DEC-[0-9]+)` + // display ID
		`$`,
)

// BypassRationaleEnvVar is the env var that must be set to a non-empty value
// when GG_ENFORCEMENT=off. Format: "TASK-NNN: reason" or just "reason".
// When a task-scoped gate is bypassed (taskID non-empty), the TASK-NNN prefix
// in the rationale is validated to match that task — a rationale written for a
// different task is rejected so cross-task rationale recycling is caught.
const BypassRationaleEnvVar = "GG_BYPASS_RATIONALE" //nolint:gosec // Policy env var name, not a secret.

// BypassRationaleRecordEnvVar is the integrity-grade companion to BypassRationaleEnvVar.
// When set, it must contain a real gg record ID (UUID or "DEC-*" prefix).
// Either env var satisfies the gate; the record form provides an auditable FK
// into the brain so the rationale is permanently searchable via gg search.
const BypassRationaleRecordEnvVar = "GG_BYPASS_RATIONALE_RECORD" //nolint:gosec // Policy env var name, not a secret.

// BypassRationaleResult is the outcome of CheckBypassRationale.
type BypassRationaleResult struct {
	// Rationale is the trimmed value of GG_BYPASS_RATIONALE.
	Rationale string
	// RationaleTaskID is the TASK-NNN parsed from the rationale prefix (e.g. "TASK-317").
	// Empty when the rationale has no TASK prefix or when taskID is empty.
	RationaleTaskID string
	// RationaleRecordID is the trimmed value of GG_BYPASS_RATIONALE_RECORD, when set.
	// Callers store this as a FK into the brain (gg record UUID or display ID).
	// Empty when GG_BYPASS_RATIONALE_RECORD was not set.
	RationaleRecordID string
}

// CheckBypassRationale validates that at least one of GG_BYPASS_RATIONALE or
// GG_BYPASS_RATIONALE_RECORD is set and, for task-scoped gates, that the
// rationale's TASK-NNN prefix matches taskID.
//
// Either env var satisfies the gate:
//   - GG_BYPASS_RATIONALE — free-form rationale text (ergonomic form).
//   - GG_BYPASS_RATIONALE_RECORD — a real gg record ID (integrity-grade form;
//     provides a queryable FK into the brain).
//
// Returns an error when:
//   - Both env vars are empty → silent bypass rejected
//   - taskID is non-empty AND the rationale carries a TASK-NNN prefix that
//     does not match taskID → cross-task rationale recycling rejected
//
// Returns the parsed result on success.
func CheckBypassRationale(taskID string) (BypassRationaleResult, error) {
	raw := strings.TrimSpace(os.Getenv(BypassRationaleEnvVar))
	recordID := strings.TrimSpace(os.Getenv(BypassRationaleRecordEnvVar))

	// Validate record ID format when provided — reject garbage strings early
	// so callers can't accidentally use a free-form rationale via the wrong var.
	if recordID != "" && !validRecordIDRe.MatchString(recordID) {
		return BypassRationaleResult{}, fmt.Errorf( //nolint:staticcheck // Multi-line CLI remediation text intentionally includes command punctuation.
			"invalid %s value %q: must be a UUID (8-4-4-4-12 hex) or display ID (DEC-NNN)\n"+
				"Use %s for free-form rationale text instead:\n\n"+
				"  %s=%q gg task done %s ...",
			BypassRationaleRecordEnvVar, recordID,
			BypassRationaleEnvVar,
			BypassRationaleEnvVar, taskID+" : <why>", taskID)
	}

	if raw == "" && recordID == "" {
		msg := fmt.Sprintf(
			"silent bypass rejected: GG_ENFORCEMENT=off requires %s=\"<reason>\" (or %s=<record-id>).\n"+
				"Provide a rationale so the bypass is auditable:\n\n"+
				"  %s=\"%s: <why this bypass is necessary>\" gg task done %s ...\n\n"+
				"For integrity-grade bypasses link a brain record:\n"+
				"  %s=<record-id> gg task done %s ...\n\n"+
				"This is TASK-317 enforcement — silent master bypasses are forbidden.\n"+
				"See CLAUDE.md § Master bypass discipline for the full policy.",
			BypassRationaleEnvVar, BypassRationaleRecordEnvVar,
			BypassRationaleEnvVar, taskID, taskID,
			BypassRationaleRecordEnvVar, taskID)
		return BypassRationaleResult{}, fmt.Errorf("%s", msg)
	}

	rationaleTaskID := extractTaskIDFromRationale(raw)

	// When the gate is task-scoped AND the rationale has a TASK prefix,
	// the prefix must match the task being closed. This catches the pattern
	// where an agent re-uses an old rationale from a different task.
	if taskID != "" && rationaleTaskID != "" && !strings.EqualFold(rationaleTaskID, taskID) {
		return BypassRationaleResult{}, fmt.Errorf( //nolint:staticcheck // Multi-line CLI remediation text intentionally includes command punctuation.
			"bypass rationale task mismatch: %s=%q references %s but gate is for %s\n"+
				"Update the rationale to reference the correct task:\n\n"+
				"  %s=\"%s: <why>\" gg task done %s ...",
			BypassRationaleEnvVar, raw, rationaleTaskID, taskID,
			BypassRationaleEnvVar, taskID, taskID)
	}

	return BypassRationaleResult{
		Rationale:         raw,
		RationaleTaskID:   rationaleTaskID,
		RationaleRecordID: recordID,
	}, nil
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
