package cmd

import (
	"errors"
	"strings"
	"syscall"
)

// sandboxPermissionHint returns a hint string when err indicates a sandbox
// TCP permission denial (EPERM or EACCES). Returns empty string for other
// errors (connection refused, timeout, etc.).
func sandboxPermissionHint(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EACCES) {
		return "→ if running under an agent harness, rerun this command outside the sandbox or with escalated permission"
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "operation not permitted") {
		return "→ if running under an agent harness, rerun this command outside the sandbox or with escalated permission"
	}
	return ""
}

// isSandboxPermissionError reports whether the error is a sandbox TCP permission denial.
func isSandboxPermissionError(err error) bool {
	return sandboxPermissionHint(err) != ""
}
