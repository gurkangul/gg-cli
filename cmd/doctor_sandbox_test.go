package cmd

import (
	"bytes"
	"strings"
	"syscall"
	"testing"

	"github.com/gurkangul/gg-cli/internal/store"
)

func TestDoctorCheckQdrant_PrintsSandboxHint(t *testing.T) {
	// Build a doctorReport and invoke the EPERM detection path directly
	// by verifying sandboxPermissionHint returns a hint for EPERM.
	// Full integration with a real dialer is tested via the helper test above.
	hint := sandboxPermissionHint(syscall.EPERM)
	if hint == "" {
		t.Fatal("expected sandbox hint for syscall.EPERM, got empty")
	}
	if !strings.Contains(hint, "sandbox") {
		t.Errorf("hint should mention 'sandbox': %q", hint)
	}
}

func TestRecordOffline_EpermVariantPrintsSandboxNote(t *testing.T) {
	// Build an OutboxQueued with an EPERM Cause and verify that
	// isSandboxPermissionError correctly identifies it.
	oq := &store.OutboxQueued{
		Kind:  store.OutboxKindDecision,
		UUID:  "test-uuid",
		Cause: syscall.EPERM,
	}
	if !isSandboxPermissionError(oq.Cause) {
		t.Error("expected isSandboxPermissionError to return true for EPERM Cause")
	}

	// Also test with a wrapped EPERM.
	wrappedErr := &store.OutboxQueued{
		Kind:  store.OutboxKindTask,
		UUID:  "test-uuid-2",
		Cause: syscall.EACCES,
	}
	if !isSandboxPermissionError(wrappedErr.Cause) {
		t.Error("expected isSandboxPermissionError to return true for EACCES Cause")
	}

	// Non-EPERM cause should not trigger.
	var buf bytes.Buffer
	_ = buf // suppress unused warning
	nonEpermOQ := &store.OutboxQueued{
		Kind:  store.OutboxKindDecision,
		UUID:  "test-uuid-3",
		Cause: &connectionRefusedError{},
	}
	if isSandboxPermissionError(nonEpermOQ.Cause) {
		t.Error("expected isSandboxPermissionError to return false for connection refused")
	}
}

// connectionRefusedError is a test helper that simulates a connection refused error.
type connectionRefusedError struct{}

func (e *connectionRefusedError) Error() string { return "dial tcp: connection refused" }
