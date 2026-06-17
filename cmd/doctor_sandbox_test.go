package cmd

import (
	"syscall"
	"testing"

	"github.com/gurkangul/gg-cli/internal/store"
)

// NOTE: TestDoctorCheckQdrantServer_PrintsSandboxHint (and its fakeQdrantChecker)
// were removed: they exercised the deleted doctorCheckQdrantServer server check
// and the removed config.Config.Qdrant field. The embedded store is always up, so
// there is no Qdrant-server health-check path to sandbox-guard. The surviving
// sandbox-permission detection is covered below.

func TestRecordOffline_EpermVariantPrintsSandboxNote(t *testing.T) {
	// OutboxQueued with EPERM Cause must be detected.
	oq := &store.OutboxQueued{
		Kind:  store.OutboxKindDecision,
		UUID:  "test-uuid",
		Cause: syscall.EPERM,
	}
	if !isSandboxPermissionError(oq.Cause) {
		t.Error("expected isSandboxPermissionError to return true for EPERM Cause")
	}

	// EACCES must also be detected.
	oq2 := &store.OutboxQueued{
		Kind:  store.OutboxKindTask,
		UUID:  "test-uuid-2",
		Cause: syscall.EACCES,
	}
	if !isSandboxPermissionError(oq2.Cause) {
		t.Error("expected isSandboxPermissionError to return true for EACCES Cause")
	}

	// Connection refused must not trigger the sandbox hint.
	oq3 := &store.OutboxQueued{
		Kind:  store.OutboxKindDecision,
		UUID:  "test-uuid-3",
		Cause: &connectionRefusedError{},
	}
	if isSandboxPermissionError(oq3.Cause) {
		t.Error("expected isSandboxPermissionError to return false for connection refused")
	}
}

// connectionRefusedError is a test helper that simulates a connection refused error.
type connectionRefusedError struct{}

func (e *connectionRefusedError) Error() string { return "dial tcp: connection refused" }
