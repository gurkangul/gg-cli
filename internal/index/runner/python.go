package runner

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// PythonRunner runs scip-python to produce a SCIP index for a Python project.
// Binary: scip-python (https://github.com/sourcegraph/scip-python)
// scip-python is installed as an npm package: npm install -g @sourcegraph/scip-python
type PythonRunner struct{}

func (*PythonRunner) Lang() Lang { return LangPython }

func (*PythonRunner) Index(ctx context.Context, req *IndexRequest) (*IndexResult, error) {
	bin, err := resolver.Resolve("scip-python")
	if err != nil {
		return nil, err
	}

	outPath, err := outputPath(req)
	if err != nil {
		return nil, err
	}

	// scip-python index --output <path> <project_root>
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, bin, "index", "--output", outPath, req.Root)
	cmd.Dir = req.Root
	cmd.Env = filteredEnv()
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("scip-python failed: %w\nstderr: %s", err, stderr.String())
	}
	return &IndexResult{IndexPath: outPath, Stderr: sanitizePythonStderr(stderr.Bytes())}, nil
}

// sanitizePythonStderr collapses scip-python 0.6.6's benign PathDistribution.name
// traceback (only surfaces on Python <3.10 because dist.name landed in 3.10) into
// a single-line notice. scip-python's 'Falling back to pip show approach' recovery
// path produces a complete index, so the 9-line stack trace is pure noise.
// https://github.com/sourcegraph/scip-python — fix not yet released as of 0.6.6.
func sanitizePythonStderr(b []byte) []byte {
	s := string(b)
	if !strings.Contains(s, "Falling back to pip show approach") ||
		!strings.Contains(s, "'PathDistribution' object has no attribute 'name'") {
		return b
	}
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	skip := false
	for _, line := range lines {
		if strings.Contains(line, "Python script failed with code 1: Traceback") {
			skip = true
			out = append(out, "note: scip-python fallback engaged (Python <3.10 package-metadata compat) — index OK")
			continue
		}
		if skip {
			if strings.Contains(line, "Falling back to pip show approach") {
				skip = false
			}
			continue
		}
		out = append(out, line)
	}
	return []byte(strings.Join(out, "\n"))
}
