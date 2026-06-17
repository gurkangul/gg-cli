package store

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gurkangul/gg-cli/internal/brain"
)

const taskSeqFile = ".task-seq"

// allocTaskID atomically allocates the next sequential task ID using a file
// lock on .gg/.task-seq so that concurrent `gg task create` invocations from
// different processes (the core multi-agent use case) never collide.
//
// On first allocation, the counter is bootstrapped from the max existing
// task_id in the vector store — so deleting the seq file recovers gracefully instead
// of reusing IDs.
func (c *Client) allocTaskID(ctx context.Context) (string, error) {
	if c.dataDir == "" {
		return "", fmt.Errorf("store client has no data dir — cannot allocate task ID")
	}
	seqPath := filepath.Join(c.dataDir, taskSeqFile)

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

	// Bootstrap: seq file is empty or zero. Try the vector store first; if unavailable,
	// fall back to scanning .gg/brain/tasks.jsonl so offline task creation works.
	if n == 0 {
		existingMax, err := c.maxTaskIDNumber(ctx)
		if err != nil {
			// the vector store down — fall back to JSONL scan to avoid reusing IDs.
			jsonlMax, jsonlErr := maxTaskIDFromBrainJSONL(c.dataDir)
			if jsonlErr != nil {
				return "", fmt.Errorf("bootstrap task seq (vector store down, jsonl fallback failed): %w", jsonlErr)
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

	return fmt.Sprintf("TASK-%03d", n), nil
}

// lockFileCtx is a context-aware exclusive lock: it polls non-blocking flock
// so Ctrl+C/ctx cancel unblocks while another process holds the lock.
// Permanent errors (not just "would block") fail fast instead of retrying.
func lockFileCtx(ctx context.Context, f *os.File) error {
	if err := tryLockFile(f); err == nil {
		return nil
	} else if !isLockContention(err) {
		return err
	}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			err := tryLockFile(f)
			if err == nil {
				return nil
			}
			if !isLockContention(err) {
				return err
			}
		}
	}
}

// maxTaskIDFromBrainJSONL scans .gg/brain/tasks.jsonl for the highest numeric
// suffix in a task_id field.  Used as a store-independent bootstrap for allocTaskID.
// Returns 0 when the file is absent or empty (safe: next allocation is TASK-001).
func maxTaskIDFromBrainJSONL(ggDir string) (int, error) {
	entries, err := brain.ReadAll(ggDir, "tasks")
	if err != nil {
		return 0, err
	}
	maxNum := 0
	for _, e := range entries {
		if id, ok := e.Payload["task_id"].(string); ok {
			if n, parseErr := ParseTaskID(id); parseErr == nil && n > maxNum {
				maxNum = n
			}
		}
	}
	return maxNum, nil
}
