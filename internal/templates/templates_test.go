// Package templates — invariant tests for the shell scripts shipped as
// starter hooks. The installer substitutes placeholders and writes each
// template verbatim, so missing trailing newlines or broken shebang lines
// would propagate to every installed user script.
package templates

import (
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
