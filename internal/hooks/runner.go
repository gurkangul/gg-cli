// Package hooks runs project-level shell hooks from .gg/hooks/<name>.d/.
// Hooks are plain shell scripts — *.sh files executed in lexicographic order.
// This is deliberately boring technology (no daemon, no git hooks, no framework).
package hooks

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Result is the outcome of running one hook script.
type Result struct {
	Script   string
	ExitCode int
	Output   string // combined stdout+stderr
	Err      error
}

// RunHooks executes all *.sh scripts in ggDir/hooks/<name>.d/ in lexicographic
// order. env vars are merged with the current process environment and passed to
// each script.
//
// If strict is false (warn-only), failures are reported but RunHooks returns nil.
// If strict is true, the first hook failure causes RunHooks to return an error
// and subsequent hooks are skipped.
//
// Output from each hook is written to out (typically os.Stderr).
func RunHooks(ggDir, name string, env map[string]string, strict bool, out io.Writer) ([]Result, error) {
	hookDir := filepath.Join(ggDir, "hooks", name+".d")
	entries, err := os.ReadDir(hookDir)
	if os.IsNotExist(err) {
		return nil, nil // no hook dir — not an error
	}
	if err != nil {
		return nil, fmt.Errorf("read hook dir %s: %w", hookDir, err)
	}

	var scripts []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sh") {
			continue
		}
		info, statErr := e.Info()
		if statErr != nil {
			continue
		}
		// Only run executable scripts.  In strict mode a non-executable script is a
		// configuration error — it signals intent to enforce but missing chmod +x.
		// Fail closed so the gate cannot be silently bypassed by accident.
		if info.Mode()&0o111 == 0 {
			if strict {
				return nil, fmt.Errorf("hook %s is not executable (mode %s): run 'chmod +x .gg/hooks/%s.d/%s' to fix, or delete the file to remove it",
					e.Name(), info.Mode(), name, e.Name())
			}
			continue
		}
		scripts = append(scripts, filepath.Join(hookDir, e.Name()))
	}
	sort.Strings(scripts) // lexicographic — 01-first.sh before 10-second.sh

	// Build env slice: inherit current process env, then overlay caller's vars.
	baseEnv := os.Environ()
	for k, v := range env {
		baseEnv = append(baseEnv, k+"="+v)
	}

	var results []Result
	for _, script := range scripts {
		cmd := exec.Command("/bin/sh", script)
		cmd.Env = baseEnv
		cmd.Dir = filepath.Dir(ggDir) // project root (parent of .gg/)
		combined, runErr := cmd.CombinedOutput()

		exitCode := 0
		if runErr != nil {
			if exitErr, ok := runErr.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			} else {
				exitCode = -1
			}
		}

		r := Result{
			Script:   filepath.Base(script),
			ExitCode: exitCode,
			Output:   string(combined),
			Err:      runErr,
		}
		results = append(results, r)

		if runErr != nil {
			if strict {
				fmt.Fprintf(out, "⚠ hook %s failed (exit %d):\n%s\n", r.Script, r.ExitCode, strings.TrimSpace(r.Output))
				return results, fmt.Errorf("hook %s failed (exit %d)", r.Script, r.ExitCode)
			}
			fmt.Fprintf(out, "⚠ advisory hook %s reported findings (exit %d):\n%s\n", r.Script, r.ExitCode, strings.TrimSpace(r.Output))
		}
	}
	return results, nil
}
