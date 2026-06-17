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

const discSeqFile = ".disc-seq"

// allocDiscID atomically allocates the next DISC-NNN using a file lock on
// .gg/.disc-seq, mirroring the task sequence pattern.
func (c *Client) allocDiscID(ctx context.Context) (string, error) {
	if c.dataDir == "" {
		return "", fmt.Errorf("store client has no data dir — cannot allocate discussion ID")
	}
	seqPath := filepath.Join(c.dataDir, discSeqFile)

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
			return "", fmt.Errorf("corrupt %s: %q — delete this file to re-bootstrap from the vector store", seqPath, s)
		}
		n = parsed
	}

	// Bootstrap: seq file empty. Try the vector store; if unavailable, fall back to the
	// JSONL source of truth (BUG-080 L4) — mirrors task/bug seq so the first DISC
	// allocation works while the vector store is down.
	if n == 0 {
		existingMax, err := c.maxDiscIDNumber(ctx)
		if err != nil {
			jsonlMax, jsonlErr := maxDiscIDFromBrainJSONL(c.dataDir)
			if jsonlErr != nil {
				return "", fmt.Errorf("bootstrap disc seq (vector store down, jsonl fallback failed): %w", jsonlErr)
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

	return fmt.Sprintf("DISC-%03d", n), nil
}

// maxDiscIDFromBrainJSONL scans .gg/brain/discussions.jsonl for the highest
// numeric DISC suffix. store-independent bootstrap for allocDiscID (BUG-080 L4).
// Returns 0 when the file is absent or empty.
func maxDiscIDFromBrainJSONL(ggDir string) (int, error) {
	entries, err := brain.ReadAll(ggDir, "discussions")
	if err != nil {
		return 0, err
	}
	maxNum := 0
	for _, e := range entries {
		if id, ok := e.Payload["disc_id"].(string); ok {
			if n, parseErr := ParseDiscID(id); parseErr == nil && n > maxNum {
				maxNum = n
			}
		}
	}
	return maxNum, nil
}
