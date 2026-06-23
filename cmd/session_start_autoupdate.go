package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/config"
)

// autoUpdateThrottleInterval bounds how often session-start hits the network for
// a self-update check. We record the last-check time in ~/.gg/update-check.json
// and skip silently within this window so a fresh shell never re-fetches.
const autoUpdateThrottleInterval = 24 * time.Hour

// autoUpdateFetchTimeout is the hard bound on the latest-tag lookup during
// session-start. Kept short so an offline/slow network never noticeably slows
// the briefing.
const autoUpdateFetchTimeout = 3 * time.Second

// autoUpdateCheckFile is the throttle state filename under ~/.gg/.
const autoUpdateCheckFile = "update-check.json"

// autoUpdateState persists the last-check timestamp for throttling.
type autoUpdateState struct {
	LastCheckUnix int64 `json:"last_check_unix"`
}

// autoUpdateNow is overridable in tests so throttle windows are deterministic.
var autoUpdateNow = time.Now

// autoUpdateStateDir resolves the directory holding the throttle file (~/.gg).
// Overridable in tests so they never touch the real home directory.
var autoUpdateStateDir = config.SharedDir

// autoUpdateIfDue runs a best-effort, throttled self-update at session-start.
// It is FAST, non-blocking-on-failure, and MUST NOT break or noticeably slow
// session-start:
//
//   - Skips when GG_NO_AUTO_UPDATE or GG_PIN_VERSION is set, on a source/dev
//     build (protects the maintainer's --from-source binary), or when the last
//     check was <24h ago.
//   - Otherwise fetches the latest tag (hard ~3s timeout). If newer, runs the
//     SAME self-update routine `gg update` uses and prints ONE concise line.
//   - Always records a new last-check timestamp (even on no-op/error) so we do
//     not re-hit the network every shell.
//   - Swallows ALL errors — session-start succeeds regardless.
func autoUpdateIfDue(cmd *cobra.Command, stdout, stderr io.Writer) {
	if autoUpdateDisabled() {
		return
	}
	current := resolveVersion()
	if isDevBuild(current) {
		// Never auto-clobber a source/dev build (e.g. the maintainer's
		// --from-source binary). The dev build opts into releases via --force.
		return
	}

	stateDir, err := autoUpdateStateDir()
	if err != nil {
		// Can't resolve ~/.gg — skip silently; nothing to throttle against.
		return
	}
	statePath := filepath.Join(stateDir, autoUpdateCheckFile)
	if !autoUpdateDue(statePath, autoUpdateNow()) {
		return
	}
	// Record the attempt up-front so a hang/crash mid-fetch still throttles the
	// next shell. Best-effort: a write failure must not abort session-start.
	_ = writeAutoUpdateState(statePath, autoUpdateNow())

	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	fctx, cancel := context.WithTimeout(ctx, autoUpdateFetchTimeout)
	defer cancel()

	rel, err := fetchLatestRelease(fctx)
	if err != nil {
		// Offline, rate-limited, slow — swallow. Throttle timestamp already
		// written above so we won't retry until the next window.
		return
	}
	cmpRes, ok := compareSemver(current, rel.TagName)
	if !ok || cmpRes >= 0 {
		return // not comparable or already current/newer
	}

	res, err := selfUpdateToRelease(ctx, rel, current)
	if err != nil {
		return // any failure leaves the existing binary untouched; stay silent
	}
	fmt.Fprintf(stdout, "✓ gg auto-updated %s → %s (set GG_NO_AUTO_UPDATE=1 to disable)\n", res.OldVersion, res.NewVersion)
}

// autoUpdateDisabled reports whether the session-start auto-update is opted out
// via GG_NO_AUTO_UPDATE (truthy) or pinned via GG_PIN_VERSION (any non-empty).
func autoUpdateDisabled() bool {
	if envTruthy(os.Getenv("GG_NO_AUTO_UPDATE")) {
		return true
	}
	if strings.TrimSpace(os.Getenv("GG_PIN_VERSION")) != "" {
		return true
	}
	return false
}

// autoUpdateDue reports whether enough time has elapsed since the last recorded
// check. A missing/unreadable/corrupt state file is treated as "due" so the
// first run always checks.
func autoUpdateDue(statePath string, now time.Time) bool {
	data, err := os.ReadFile(statePath)
	if err != nil {
		return true
	}
	var st autoUpdateState
	if err := json.Unmarshal(data, &st); err != nil || st.LastCheckUnix == 0 {
		return true
	}
	last := time.Unix(st.LastCheckUnix, 0)
	return now.Sub(last) >= autoUpdateThrottleInterval
}

// writeAutoUpdateState records now as the last-check time, creating ~/.gg if
// needed. Best-effort: callers ignore the error (a failed write just means the
// next shell re-checks).
func writeAutoUpdateState(statePath string, now time.Time) error {
	if err := os.MkdirAll(filepath.Dir(statePath), 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(autoUpdateState{LastCheckUnix: now.Unix()})
	if err != nil {
		return err
	}
	return os.WriteFile(statePath, data, 0o600)
}
