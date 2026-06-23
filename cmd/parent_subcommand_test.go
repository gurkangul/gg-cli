package cmd

import (
	"strings"
	"testing"
)

// BUG-093: parent commands (`gg task`, `gg bug`, `gg telemetry`) used to fall
// through to help text and exit 0 when invoked with no subcommand or an unknown
// subcommand, silently misleading fresh agents. They must now error non-zero.
// The routing guards run before any backend access, so these cases need no .gg
// dir (the bare-telemetry summary case is the one exception and sets one up).

func TestTaskParent_NoSubcommandErrors(t *testing.T) {
	t.Chdir(t.TempDir())
	_, _, err := execCmd(t, "task")
	if err == nil {
		t.Fatal("bare `gg task` must error, not fall through to help with exit 0")
	}
}

func TestTaskParent_UnknownSubcommandErrors(t *testing.T) {
	t.Chdir(t.TempDir())
	_, _, err := execCmd(t, "task", "frob")
	if err == nil {
		t.Fatal("`gg task frob` must error on the unknown subcommand")
	}
	if !strings.Contains(err.Error(), "unknown task subcommand") {
		t.Fatalf("expected an unknown-subcommand error, got %v", err)
	}
}

// `gg task show TASK-999` must route to the `show`→`get` alias (BUG-093 / the
// original report's `gg task show`). With a .gg dir and no such task, the get
// path errors (not found) — proving it routed there. Pre-fix it fell through to
// help and exited 0 (err == nil), so a non-nil non-unknown error is the
// discriminator.
func TestTaskParent_ShowAliasesToGet(t *testing.T) {
	setupGGDir(t)
	_, _, err := execCmd(t, "task", "show", "TASK-999")
	if err == nil {
		t.Fatal("`gg task show TASK-999` must route to `gg task get` (which errors for a missing task), not fall through to help with exit 0")
	}
	if strings.Contains(err.Error(), "unknown task subcommand") {
		t.Fatalf("`gg task show` must alias to `gg task get`, not be rejected as unknown: %v", err)
	}
}

func TestBugParent_NoSubcommandErrors(t *testing.T) {
	t.Chdir(t.TempDir())
	_, _, err := execCmd(t, "bug")
	if err == nil {
		t.Fatal("bare `gg bug` must error, not fall through to help with exit 0")
	}
}

func TestBugParent_UnknownSubcommandErrors(t *testing.T) {
	t.Chdir(t.TempDir())
	_, _, err := execCmd(t, "bug", "frob")
	if err == nil {
		t.Fatal("`gg bug frob` must error on the unknown subcommand")
	}
	if !strings.Contains(err.Error(), "unknown bug subcommand") {
		t.Fatalf("expected an unknown-subcommand error, got %v", err)
	}
}

// `gg bug show BUG-999` must route to the `show`→`get` alias, not fall through
// to help (the BUG-093 report explicitly flags `gg bug show` as wrongly falling
// through). With a .gg dir and no such bug, the get path errors — pre-fix it
// fell through to help and exited 0 (err == nil).
func TestBugParent_ShowAliasesToGet(t *testing.T) {
	setupGGDir(t)
	_, _, err := execCmd(t, "bug", "show", "BUG-999")
	if err == nil {
		t.Fatal("`gg bug show BUG-999` must route to `gg bug get` (which errors for a missing bug), not fall through to help with exit 0")
	}
	if strings.Contains(err.Error(), "unknown bug subcommand") {
		t.Fatalf("`gg bug show` must alias to `gg bug get`, not be rejected as unknown: %v", err)
	}
}

func TestTelemetryParent_UnknownSubcommandErrors(t *testing.T) {
	t.Chdir(t.TempDir())
	_, _, err := execCmd(t, "telemetry", "frob")
	if err == nil {
		t.Fatal("`gg telemetry frob` must error on the unknown subcommand, not run the summary")
	}
	if !strings.Contains(err.Error(), "unknown telemetry subcommand") {
		t.Fatalf("expected an unknown-subcommand error, got %v", err)
	}
}

// Bare `gg telemetry` must KEEP defaulting to the summary view (BUG-093 chose
// "default to summary" over erroring) — the unknown-subcommand guard must not
// reject the zero-arg case. With a valid .gg dir and no telemetry data it
// prints the empty-summary line and exits 0.
func TestTelemetryParent_BareRunsSummary(t *testing.T) {
	setupGGDir(t)
	_, _, err := execCmd(t, "telemetry")
	if err != nil {
		t.Fatalf("bare `gg telemetry` should run the summary (exit 0), got %v", err)
	}
}
