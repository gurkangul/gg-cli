package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// indexLockFile guards a one-shot `gg index` run so overlapping runs never write
// the graph at the same time. The detached git hooks (see doctor_index_hooks.go)
// fire-and-forget `gg index --changed`, so a quick commit→push can start a second
// run before the first finishes; without this guard they would race the graph DB.
const indexLockFile = "index.lock"

// indexLockStaleAfter bounds how long a one-shot index lock is honored. A
// `gg index` run self-aborts at its 10-minute context cap (cmd/index.go), so a
// live-PID lock older than this was leaked by an unclean kill (SIGKILL/OOM/power
// loss) and its PID may since have been reused by an unrelated same-user
// process. Honoring such a phantom would wedge indexing — and the documented
// `gg doctor --fix-index` repair — forever, so past this age the lock is
// reclaimed regardless of PID. The margin over 10m absorbs clock skew.
const indexLockStaleAfter = 15 * time.Minute

// indexLock records the one-shot `gg index` run that currently owns the graph
// write. It mirrors indexWatchLock (index_watch.go) so debugging sees the same
// shape, but lives in its own file so a live foreground watcher and a hook-driven
// one-shot index never both write the graph at once.
type indexLock struct {
	PID       int    `json:"pid"`
	StartedAt string `json:"started_at"`
	Lang      string `json:"lang"`
	Root      string `json:"root"`
}

// acquireIndexLock takes the one-shot index lock for root/lang. It returns
// (release, true, nil) when the caller owns the lock and must run, or
// (nil, false, nil) when another live index already owns the graph write —
// either a one-shot run (.gg/index.lock) or a foreground watcher
// (.gg/index-watch.lock). In the not-acquired case the caller should skip: a
// fire-and-forget git hook must not block, and the running index will cover the
// newer delta (or the next git op re-fires the hook). Stale locks whose PID is no
// longer running are reclaimed.
func acquireIndexLock(ggDir, root, lang string) (func(), bool, error) {
	// A live foreground watcher owns indexing for this project; skip so a hook's
	// one-shot run never races the watcher's internal reindex. The watcher runs
	// indefinitely (until Ctrl-C), so it is honored purely on PID liveness — no
	// staleness cap, unlike the 10-minute-capped one-shot lock below.
	if w, ok := readIndexWatchLock(filepath.Join(ggDir, indexWatchLockFile)); ok && processRunning(w.PID) {
		return nil, false, nil
	}
	path := filepath.Join(ggDir, indexLockFile)
	if existing, ok := readIndexLock(path); ok && oneShotLockHeld(existing) {
		return nil, false, nil
	}
	_ = os.Remove(path) // clear a stale lock from a dead PID before reclaiming

	data, err := json.MarshalIndent(indexLock{
		PID:       os.Getpid(),
		StartedAt: time.Now().UTC().Format(time.RFC3339),
		Lang:      lang,
		Root:      root,
	}, "", "  ")
	if err != nil {
		return nil, false, err
	}
	// O_EXCL closes the check→create race: if another index created the lock in the
	// gap above, we lose the race and skip rather than clobber it.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if os.IsExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("create index lock: %w", err)
	}
	if _, err := f.Write(append(data, '\n')); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return nil, false, fmt.Errorf("write index lock: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return nil, false, fmt.Errorf("close index lock: %w", err)
	}

	released := false
	return func() {
		if released {
			return
		}
		released = true
		_ = os.Remove(path)
	}, true, nil
}

// readIndexLock loads the one-shot index lock; the second return is false when
// the file is absent, unparsable, or missing a PID.
func readIndexLock(path string) (indexLock, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return indexLock{}, false
	}
	var l indexLock
	if err := json.Unmarshal(data, &l); err != nil {
		return indexLock{}, false
	}
	return l, l.PID > 0
}

// oneShotLockHeld reports whether a one-shot index lock represents a genuinely
// active run: its PID must be alive AND the lock must be younger than
// indexLockStaleAfter. Beyond that age it is a leaked/phantom lock (possibly a
// reused PID) and is treated as free. An unparsable StartedAt falls back to the
// liveness check alone so a lock written by an older gg is not eternally honored.
func oneShotLockHeld(l indexLock) bool {
	if !processRunning(l.PID) {
		return false
	}
	if t, err := time.Parse(time.RFC3339, l.StartedAt); err == nil {
		return time.Since(t) <= indexLockStaleAfter
	}
	return true
}

// indexLockActive reports whether any index is currently writing this project's
// graph — a live one-shot `gg index` (index.lock) or a foreground watcher
// (index-watch.lock). `gg doctor --fix-index` uses it to distinguish "the graph
// is stale because a refresh is in flight" (informational, retry) from a genuine
// repair failure, instead of hard-failing when its own run was lock-skipped.
func indexLockActive(ggDir string) bool {
	if w, ok := readIndexWatchLock(filepath.Join(ggDir, indexWatchLockFile)); ok && processRunning(w.PID) {
		return true
	}
	if l, ok := readIndexLock(filepath.Join(ggDir, indexLockFile)); ok && oneShotLockHeld(l) {
		return true
	}
	return false
}
