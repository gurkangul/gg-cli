package cmd

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"syscall"
	"testing"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/store"
)

// fakeQdrantChecker is an injectable fake that returns a canned error from HealthCheck.
type fakeQdrantChecker struct{ err error }

func (f *fakeQdrantChecker) HealthCheck(_ context.Context) error { return f.err }
func (f *fakeQdrantChecker) CollectionStatus(_ context.Context) ([]string, []string, error) {
	return nil, nil, nil
}
func (f *fakeQdrantChecker) Close() error { return nil }

func TestDoctorCheckQdrant_PrintsSandboxHint(t *testing.T) {
	// Inject a fake client that returns EPERM on HealthCheck.
	orig := doctorQdrantNewClient
	defer func() { doctorQdrantNewClient = orig }()
	doctorQdrantNewClient = func(_ *config.Config, _ string) (qdrantHealthChecker, error) {
		return &fakeQdrantChecker{err: syscall.EPERM}, nil
	}

	// Capture stderr.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	origStderr := os.Stderr
	os.Stderr = w

	report := &doctorReport{}
	cfg := &config.Config{}
	cfg.Qdrant.Host = "localhost"
	cfg.Qdrant.Port = 6334

	cmd := &cobra.Command{}
	cmd.SetContext(t.Context())
	doctorCheckQdrant(cmd, cfg, report)

	w.Close()
	os.Stderr = origStderr
	var buf bytes.Buffer
	io.Copy(&buf, r) //nolint:errcheck

	if report.problems != 1 {
		t.Fatalf("expected 1 problem, got %d", report.problems)
	}
	output := buf.String()
	if !strings.Contains(output, "operation not permitted (sandbox?)") {
		t.Errorf("expected sandbox banner in stderr, got: %q", output)
	}
	if !strings.Contains(output, "sandbox") {
		t.Errorf("expected hint line mentioning sandbox, got: %q", output)
	}
}

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
