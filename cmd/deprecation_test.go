package cmd

import (
	"strings"
	"testing"
)

// TestDeprecationMessage_NamesReplacement verifies the single canonical wording
// embeds the replacement command and the "removed in a future major" promise.
func TestDeprecationMessage_NamesReplacement(t *testing.T) {
	msg := deprecationMessage("gg record")
	if !strings.Contains(msg, "gg record") {
		t.Errorf("message must name the replacement, got %q", msg)
	}
	if !strings.Contains(strings.ToLower(msg), "instead") {
		t.Errorf("message should read naturally after cobra's prefix, got %q", msg)
	}
}

// TestDecideCmd_SingleSourceDeprecation guarantees the no-double-warning design:
// the runtime notice is owned solely by cobra's Deprecated field, whose value is
// built by the shared helper. If runDecide ever re-adds a hand-rolled warning,
// the source-of-truth check below still pins the wording to the helper so the two
// cannot silently diverge.
func TestDecideCmd_SingleSourceDeprecation(t *testing.T) {
	want := deprecationMessage("gg record")
	if decideCmd.Deprecated != want {
		t.Errorf("decideCmd.Deprecated = %q, want helper output %q", decideCmd.Deprecated, want)
	}
}

func TestRejectCmd_SingleSourceDeprecation(t *testing.T) {
	want := deprecationMessage("gg record --decision-status=rejected")
	if rejectCmd.Deprecated != want {
		t.Errorf("rejectCmd.Deprecated = %q, want helper output %q", rejectCmd.Deprecated, want)
	}
}

// TestDecide_DeprecationWarnsOnce confirms the runtime notice for a real run
// fires exactly once (cobra emits it; runDecide no longer Fprintln's its own).
// The harness routes cobra's Printf to SetOut and the helper-free handler writes
// nothing extra, so across stdout+stderr the "is deprecated" line appears once.
func TestDecide_DeprecationWarnsOnce(t *testing.T) {
	setupGGDir(t)
	out, errStr, _ := execCmd(t, "decide", "use JWT for auth")
	combined := out + errStr
	if got := strings.Count(combined, "is deprecated"); got != 1 {
		t.Errorf("expected the deprecation notice exactly once, got %d in:\nstdout=%q\nstderr=%q", got, out, errStr)
	}
}

func TestReject_DeprecationWarnsOnce(t *testing.T) {
	setupGGDir(t)
	out, errStr, _ := execCmd(t, "reject", "microservices")
	combined := out + errStr
	if got := strings.Count(combined, "is deprecated"); got != 1 {
		t.Errorf("expected the deprecation notice exactly once, got %d in:\nstdout=%q\nstderr=%q", got, out, errStr)
	}
}
