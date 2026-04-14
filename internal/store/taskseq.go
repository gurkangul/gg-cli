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

	f, err := os.OpenFile(seqPath, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", seqPath, err)
	}
	defer f.Close()

	if err := lockFileCtx(ctx, f); err != nil {
		return "", fmt.Errorf("lock %s: %w", seqPath, err)
	}
	defer unlockFile(f)

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

	// Bootstrap: seq file is empty or zero. Pick up from max existing task
	// in Qdrant so we don't reuse IDs after a seq-file wipe. Any error here
	// must fail the allocation — proceeding with n=0 would silently overwrite
	// existing tasks via the deterministic point UUID.
	if n == 0 {
		existingMax, err := c.maxTaskIDNumber(ctx)
		if err != nil {
			return "", fmt.Errorf("bootstrap task seq from qdrant: %w", err)
		}
		n = existingMax
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
func lockFileCtx(ctx context.Context, f *os.File) error {
	// Fast path.
	if err := tryLockFile(f); err == nil {
		return nil
	}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := tryLockFile(f); err == nil {
				return nil
			}
		}
	}
}
