package runner

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
)

// TypeScriptRunner runs scip-typescript to produce a SCIP index for a
// TypeScript or JavaScript project.
// Binary: scip-typescript (https://github.com/sourcegraph/scip-typescript)
// Install: npm install -g @sourcegraph/scip-typescript
type TypeScriptRunner struct{}

func (*TypeScriptRunner) Lang() Lang { return LangTypeScript }

func (*TypeScriptRunner) Index(ctx context.Context, req *IndexRequest) (*IndexResult, error) {
	bin, err := resolver.Resolve("scip-typescript")
	if err != nil {
		return nil, err
	}

	outPath, cleanup, err := outputPath(req)
	if err != nil {
		return nil, err
	}
	if cleanup != nil {
		defer cleanup()
	}

	// scip-typescript index --output <path>
	// Must run from project root so it picks up tsconfig.json.
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, bin, "index", "--output", outPath)
	cmd.Dir = req.Root
	cmd.Env = filteredEnv()
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("scip-typescript failed: %w\nstderr: %s", err, stderr.String())
	}
	return &IndexResult{IndexPath: outPath, Stderr: stderr.Bytes()}, nil
}
