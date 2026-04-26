//go:build sandbox_test

package cmd

import (
	"bytes"
	"net"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

// TestDiagnoseSandbox_DetectsBlockedTCP injects a dialer that returns EPERM and
// verifies runDoctorDiagnoseSandbox emits the BLOCKED message. Build-tag gated
// so it is skipped in normal CI; run with -tags sandbox_test for sandbox environments.
func TestDiagnoseSandbox_DetectsBlockedTCP(t *testing.T) {
	orig := sandboxDialer
	defer func() { sandboxDialer = orig }()
	sandboxDialer = func(_, _ string, _ time.Duration) (net.Conn, error) {
		return nil, syscall.EPERM
	}

	var buf bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&buf)
	cmd.SetContext(t.Context())

	if err := runDoctorDiagnoseSandbox(cmd); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "TCP localhost BLOCKED") {
		t.Errorf("expected BLOCKED message, got: %q", output)
	}
	if !strings.Contains(output, "escalate permissions") {
		t.Errorf("expected escalate hint, got: %q", output)
	}
}

// TestDiagnoseSandbox_PermittedWhenRefused verifies connection-refused is treated as permitted.
func TestDiagnoseSandbox_PermittedWhenRefused(t *testing.T) {
	orig := sandboxDialer
	defer func() { sandboxDialer = orig }()
	sandboxDialer = func(_, _ string, _ time.Duration) (net.Conn, error) {
		return nil, &net.OpError{Op: "dial", Err: &connectionRefusedSyscallError{}}
	}

	var buf bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&buf)
	cmd.SetContext(t.Context())

	if err := runDoctorDiagnoseSandbox(cmd); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "TCP localhost permitted") {
		t.Errorf("expected permitted message, got: %q", output)
	}
}

type connectionRefusedSyscallError struct{}

func (e *connectionRefusedSyscallError) Error() string   { return "connection refused" }
func (e *connectionRefusedSyscallError) Timeout() bool   { return false }
func (e *connectionRefusedSyscallError) Temporary() bool { return false }
