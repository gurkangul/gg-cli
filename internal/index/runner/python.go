package runner

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
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
	return &IndexResult{IndexPath: outPath, Stderr: stderr.Bytes()}, nil
}
