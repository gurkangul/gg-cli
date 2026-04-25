// Package templates — invariant tests for the shell scripts shipped as
// starter hooks. The installer substitutes placeholders and writes each
// template verbatim, so missing trailing newlines or broken shebang lines
// would propagate to every installed user script.
package templates

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// Each template MUST end with a trailing newline so strings.ReplaceAll
// substitutions and `cat >>` concatenations don't corrupt the final line.
// A missing newline would leave installed scripts without proper line
// termination — harmless in POSIX most of the time, catastrophic when the
// last line is the only non-comment statement.
func TestTemplates_TrailingNewline(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"TaskDoneGoHook", TaskDoneGoHook},
		{"PreTaskDoneGoHook", PreTaskDoneGoHook},
		{"PreTaskDoneNodeHook", PreTaskDoneNodeHook},
		{"BugReprosHook", BugReprosHook},
		{"SmokeE2EHook", SmokeE2EHook},
		{"MakefileTestTiers", MakefileTestTiers},
	}
	for _, tc := range cases {
		if !strings.HasSuffix(tc.body, "\n") {
			t.Errorf("%s: template body must end with '\\n' (violated — fix the source .sh file)", tc.name)
		}
	}
}

// Every template must begin with a POSIX shebang so the installed file is
// executable via the kernel's interpreter resolution. hooks.RunHooks invokes
// /bin/sh on the script, so the shebang is advisory — but the installer's
// chmod 0755 + kernel exec path is the source-of-truth contract for users
// who may run the script outside gg.
func TestTemplates_Shebang(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"TaskDoneGoHook", TaskDoneGoHook},
		{"PreTaskDoneGoHook", PreTaskDoneGoHook},
		{"PreTaskDoneNodeHook", PreTaskDoneNodeHook},
		{"BugReprosHook", BugReprosHook},
		{"SmokeE2EHook", SmokeE2EHook},
	}
	for _, tc := range cases {
		if !strings.HasPrefix(tc.body, "#!/bin/sh\n") {
			t.Errorf("%s: template must begin with '#!/bin/sh\\n'; got first line %q",
				tc.name, strings.SplitN(tc.body, "\n", 2)[0])
		}
	}
}

// The pre-task-done templates use __GG_SUBDIR__ as a cd target placeholder
// injected by the installer. The literal must exist in the body so the
// installer's strings.ReplaceAll has something to substitute — regressing
// this would silently break monorepo installs.
func TestTemplates_SubdirPlaceholder(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"PreTaskDoneGoHook", PreTaskDoneGoHook},
		{"PreTaskDoneNodeHook", PreTaskDoneNodeHook},
	}
	for _, tc := range cases {
		if !strings.Contains(tc.body, "__GG_SUBDIR__") {
			t.Errorf("%s: missing __GG_SUBDIR__ placeholder — monorepo installs will cd into the repo root unconditionally", tc.name)
		}
	}
}

func TestPreTaskDoneGoHook_DiagnosesGoTestFailure(t *testing.T) {
	required := []string{
		"go test -json ./...",
		"collecting JSON failure summary",
		"[verify-json]",
	}
	for _, needle := range required {
		if !strings.Contains(PreTaskDoneGoHook, needle) {
			t.Errorf("PreTaskDoneGoHook missing %q; failed hook output may end with opaque bare FAIL", needle)
		}
	}
}

// TestHookEnvContract verifies that the canonical hook env-var reference
// (docs/hook-env-vars.md) stays in sync with the shell scripts under
// internal/templates/. It catches two classes of drift:
//
//  1. A variable read by a hook script is not documented — future agents
//     cannot discover it and may disable the gate by accident.
//  2. A variable documented in the hook-injected section is never read by
//     any script — dead documentation causes the same confusion in reverse.
//
// Variables documented as CLI-level (bypass, telemetry, identity,
// observability) are intentionally not read by hook scripts; those are
// excluded from the doc→script direction check via the cliOnlyVars set.
func TestHookEnvContract(t *testing.T) {
	// Locate repo root relative to this file.
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// internal/templates/ → ../../ → repo root
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..")

	// ── Step 1: parse docs/hook-env-vars.md for every GG_* name ──────────
	docPath := filepath.Join(repoRoot, "docs", "hook-env-vars.md")
	docBytes, err := os.ReadFile(docPath)
	if err != nil {
		t.Fatalf("cannot read %s: %v", docPath, err)
	}
	varInDoc := extractGGVars(string(docBytes))

	// ── Step 2: parse all .sh files under internal/templates/ ────────────
	shFiles, err := filepath.Glob(filepath.Join(repoRoot, "internal", "templates", "*.sh"))
	if err != nil {
		t.Fatalf("glob failed: %v", err)
	}
	if len(shFiles) == 0 {
		t.Fatal("no .sh files found under internal/templates/")
	}
	varInScripts := make(map[string][]string) // varName → list of files that read it
	for _, f := range shFiles {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("cannot read %s: %v", f, err)
		}
		for v := range extractGGVars(string(data)) {
			varInScripts[v] = append(varInScripts[v], filepath.Base(f))
		}
	}

	// ── Step 3a: every script-read var must be documented ─────────────────
	t.Run("script_vars_documented", func(t *testing.T) {
		for v, files := range varInScripts {
			if !varInDoc[v] {
				t.Errorf("GG var %q is read by hook script(s) %v but is not documented in docs/hook-env-vars.md — add it to the quick-reference table", v, files)
			}
		}
	})

	// ── Step 3b: hook-injected vars must be read by at least one script ───
	// These are the five vars that taskHookEnv injects into every hook subprocess.
	// If none of the bundled scripts read them, the documentation is misleading.
	hookInjected := []string{
		"GG_TASK_ID",
		"GG_TASK_SUMMARY",
		"GG_PROJECT_ID",
		"GG_ACTOR",
		"GG_INSIDE_HOOK",
	}
	t.Run("hook_injected_vars_are_read", func(t *testing.T) {
		for _, v := range hookInjected {
			if !varInDoc[v] {
				t.Errorf("hook-injected var %q missing from docs/hook-env-vars.md", v)
			}
			if len(varInScripts[v]) == 0 {
				t.Errorf("hook-injected var %q is documented but no bundled .sh template reads it — either the doc or the scripts are stale", v)
			}
		}
	})
}

// extractGGVars returns the deduplicated set of GG_* variable names found in
// src. It matches $GG_FOO, ${GG_FOO}, and bare GG_FOO (for markdown tables).
var ggVarPattern = regexp.MustCompile(`\b(GG_[A-Z0-9_]+)\b`)

func extractGGVars(src string) map[string]bool {
	out := make(map[string]bool)
	for _, m := range ggVarPattern.FindAllStringSubmatch(src, -1) {
		out[strings.TrimRight(m[1], "_")] = true
	}
	return out
}
