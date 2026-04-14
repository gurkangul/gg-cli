package store

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const taskSeqFile = ".task-seq"

// allocTaskID atomically allocates the next sequential task ID using a file
// lock on .gg/.task-seq so that concurrent `gg task create` invocations from
// different processes (the core multi-agent use case) never collide.
//
// On first allocation, the counter is bootstrapped from the max existing
// task_id in Qdrant — so deleting the seq file recovers gracefully instead
// of reusing IDs.
func (c *Client) allocTaskID(ctx context.Context) (string, error) {
	if c.dataDir == "" {
		return "", fmt.Errorf("store client has no data dir — cannot allocate task ID")
	}
	seqPath := filepath.Join(c.dataDir, taskSeqFile)

	f, err := os.OpenFile(seqPath, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", seqPath, err)
	}
	defer f.Close()

	if err := lockFile(f); err != nil {
		return "", fmt.Errorf("lock %s: %w", seqPath, err)
	}
	defer unlockFile(f)

	data, err := io.ReadAll(f)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", seqPath, err)
	}

	n := 0
	if s := strings.TrimSpace(string(data)); s != "" {
		if parsed, perr := strconv.Atoi(s); perr == nil && parsed >= 0 {
			n = parsed
		}
	}

	// Bootstrap: if the file is missing/zero but Qdrant already has tasks,
	// pick up from there so we don't reuse IDs after a seq-file wipe.
	if n == 0 {
		if existingMax, err := c.maxTaskIDNumber(ctx); err == nil && existingMax > n {
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

	return fmt.Sprintf("TASK-%03d", n), nil
}
