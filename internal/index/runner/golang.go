package runner

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// GoRunner runs scip-go to produce a SCIP index for a Go project.
// Binary: scip-go (https://github.com/sourcegraph/scip-go)
type GoRunner struct{}

func (*GoRunner) Lang() Lang { return LangGo }

func (*GoRunner) Index(ctx context.Context, req *IndexRequest) (*IndexResult, error) {
	bin, err := resolver.Resolve("scip-go")
	if err != nil {
		return nil, err
	}

	outPath, err := outputPath(req)
	if err != nil {
		return nil, err
	}

	// scip-go --output <path> -- runs inside the project root.
	// scip-go indexes the module rooted at the working directory.
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, bin, "--output", outPath)
	cmd.Dir = req.Root
	cmd.Env = filteredEnv()
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("scip-go failed: %w\nstderr: %s", err, stderr.String())
	}
	return &IndexResult{IndexPath: outPath, Stderr: stderr.Bytes()}, nil
}

// outputPath resolves the output file path from the request.
// If req.OutputPath is set, it uses that. Otherwise it creates a temp file.
// The caller is responsible for removing the temp file when done.
func outputPath(req *IndexRequest) (string, error) {
	if req.OutputPath != "" {
		return req.OutputPath, nil
	}
	f, err := os.CreateTemp("", "gg-scip-*.scip")
	if err != nil {
		return "", fmt.Errorf("create temp scip file: %w", err)
	}
	path := filepath.Clean(f.Name())
	_ = f.Close()
	return path, nil
}
