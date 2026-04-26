package cmd

import (
	"errors"
	"fmt"
	"strings"
	"syscall"
	"testing"
)

func TestSandboxPermissionHint_RecognizesEPERM(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		wantHit bool
	}{
		{name: "syscall.EPERM", err: syscall.EPERM, wantHit: true},
		{name: "syscall.EACCES", err: syscall.EACCES, wantHit: true},
		{name: "wrapped EPERM", err: fmt.Errorf("dial tcp: %w", syscall.EPERM), wantHit: true},
		{name: "string operation not permitted", err: errors.New("connect: operation not permitted"), wantHit: true},
		{name: "connection refused", err: errors.New("dial tcp: connection refused"), wantHit: false},
		{name: "timeout", err: errors.New("context deadline exceeded"), wantHit: false},
		{name: "nil", err: nil, wantHit: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			hint := sandboxPermissionHint(tc.err)
			got := hint != ""
			if got != tc.wantHit {
				t.Errorf("sandboxPermissionHint(%v) = %q (hit=%v), want hit=%v", tc.err, hint, got, tc.wantHit)
			}
			if tc.wantHit && !strings.Contains(hint, "sandbox") {
				t.Errorf("hint should mention 'sandbox': %q", hint)
			}
		})
	}
}
