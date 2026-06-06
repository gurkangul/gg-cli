package store

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gurkangul/gg-cli/internal/brain"
)

const bugSeqFile = ".bug-seq"

// allocBugID atomically allocates the next BUG-NNN using a file lock on
// .gg/.bug-seq, mirroring the task and discussion sequence pattern.
func (c *Client) allocBugID(ctx context.Context) (string, error) {
	if c.dataDir == "" {
		return "", fmt.Errorf("store client has no data dir — cannot allocate bug ID")
	}
	seqPath := filepath.Join(c.dataDir, bugSeqFile)

	f, err := os.OpenFile(seqPath, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", seqPath, err)
	}
	defer func() { _ = f.Close() }()

	if err := lockFileCtx(ctx, f); err != nil {
		return "", fmt.Errorf("lock %s: %w", seqPath, err)
	}
	defer func() { _ = unlockFile(f) }()

	data, err := io.ReadAll(f)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", seqPath, err)
	}

	n := 0
	if s := strings.TrimSpace(string(data)); s != "" {
		parsed, perr := strconv.Atoi(s)
		if perr != nil || parsed < 0 {
			return "", fmt.Errorf("corrupt %s: %q — delete this file to re-bootstrap from qdrant", seqPath, s)
		}
		n = parsed
	}

	// Bootstrap: seq file empty. Try Qdrant; if unavailable, fall back to JSONL.
	if n == 0 {
		existingMax, err := c.maxBugIDNumber(ctx)
		if err != nil {
			jsonlMax, jsonlErr := maxBugIDFromBrainJSONL(c.dataDir)
			if jsonlErr != nil {
				return "", fmt.Errorf("bootstrap bug seq (qdrant down, jsonl fallback failed): %w", jsonlErr)
			}
			n = jsonlMax
		} else {
			n = existingMax
		}
	}

	n++

	if _, err := f.Seek(0, 0); err != nil {
		return "", err
	}
	if err := f.Truncate(0); err != nil {
		return "", err
	}
	if _, err := fmt.Fprintf(f, "%d\n", n); err != nil {
		return "", err
	}
	if err := f.Sync(); err != nil {
		return "", err
	}

	return fmt.Sprintf("BUG-%03d", n), nil
}

// maxBugIDFromBrainJSONL scans .gg/brain/bugs.jsonl for the highest numeric
// suffix. Used as a Qdrant-free bootstrap for allocBugID.
// Returns 0 when the file is absent or empty.
func maxBugIDFromBrainJSONL(ggDir string) (int, error) {
	entries, err := brain.ReadAll(ggDir, "bugs")
	if err != nil {
		return 0, err
	}
	maxNum := 0
	for _, e := range entries {
		if id, ok := e.Payload["bug_id"].(string); ok {
			if n, parseErr := ParseBugID(id); parseErr == nil && n > maxNum {
				maxNum = n
			}
		}
	}
	return maxNum, nil
}
