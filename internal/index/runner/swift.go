package runner

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
)

// SwiftRunner runs a user-provided Swift-to-SCIP converter.
//
// There is no official Sourcegraph scip-swift binary bundled by gg. The
// external binary contract is intentionally explicit and small:
//
//	scip-swift index --output <scip-file> <project-root>
//
// The command is executed with cwd set to the Swift project root and must write
// a valid SCIP file at the supplied output path. SCIP document paths must be
// either relative to that cwd or absolute paths under the gg project root;
// paths outside the project are ignored by the graph writer.
type SwiftRunner struct{}

func (*SwiftRunner) Lang() Lang { return LangSwift }

func (*SwiftRunner) Index(ctx context.Context, req *IndexRequest) (*IndexResult, error) {
	bin, err := resolver.Resolve("scip-swift")
	if err != nil {
		return nil, err
	}

	outPath, err := outputPath(req)
	if err != nil {
		return nil, err
	}

	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, bin, "index", "--output", outPath, req.Root)
	cmd.Dir = req.Root
	cmd.Env = filteredEnv()
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("scip-swift failed: %w\nstderr: %s", err, stderr.String())
	}
	return &IndexResult{IndexPath: outPath, Stderr: stderr.Bytes()}, nil
}
