package brain

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"
)

// ErrVersionConflict means a mutation's expected version did not match the
// current folded version in JSONL — another writer advanced the record first
// (BUG-063). Nothing is written; the caller should re-read and retry or surface
// the conflict.
var ErrVersionConflict = errors.New("brain: version conflict (record changed concurrently)")

// entryVersion reads the integer "version" field from a folded entry payload.
// JSON numbers decode to float64, so both float64 and int64 are accepted.
// Returns 0 for legacy records written before versioning (BUG-062/063).
func entryVersion(e Entry) int64 {
	return payloadVersionInt(e.Payload["version"])
}

// payloadVersionInt coerces a payload "version" field (any numeric JSON type)
// to int64. Exported-adjacent helper reused by store-side fold logic.
func payloadVersionInt(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	case int:
		return int64(n)
	case json.Number:
		i, _ := n.Int64()
		return i
	default:
		return 0
	}
}

// PayloadVersion returns the integer "version" field of a payload (0 if absent),
// coercing any numeric JSON type to int64.
func PayloadVersion(payload map[string]any) int64 {
	if payload == nil {
		return 0
	}
	return payloadVersionInt(payload["version"])
}

// CurrentVersion returns the version of the latest (folded) JSONL entry for id,
// or 0 when the record has no JSONL entry yet (legacy/Qdrant-only record).
func CurrentVersion(ggDir, kind, id string) (int64, error) {
	entries, err := ReadLatest(ggDir, kind)
	if err != nil {
		return 0, err
	}
	for _, e := range entries {
		if e.UUID == id {
			return entryVersion(e), nil
		}
	}
	return 0, nil
}

// AppendCAS atomically appends a mutation line for an existing uuid, enforcing
// optimistic concurrency on the payload "version" field. It holds an exclusive
// flock for the whole read-verify-append window so two concurrent PIDs cannot
// both win (BUG-062/063): JSONL is the durable source of truth and the CAS gate;
// Qdrant is mirrored best-effort by the caller afterwards.
//
// expectedVersion is the version the caller observed (0 for legacy records with
// no version field yet). On success the line is written with
// payload["version"] = expectedVersion+1 and the new version is returned.
// On mismatch ErrVersionConflict is returned and nothing is written.
//
// The caller passes the FULL current payload (all fields) so that fold/replay
// reconstructs complete state — a partial payload would drop fields on rebuild.
func AppendCAS(ggDir, kind, id, author string, expectedVersion int64, payload map[string]any) (int64, error) {
	if id == "" {
		return 0, fmt.Errorf("brain: AppendCAS requires a uuid")
	}
	if payload == nil {
		payload = map[string]any{}
	}
	if err := os.MkdirAll(dir(ggDir), 0o755); err != nil {
		return 0, fmt.Errorf("brain: mkdir: %w", err)
	}
	f, err := os.OpenFile(kindPath(ggDir, kind), os.O_APPEND|os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return 0, fmt.Errorf("brain: open %s: %w", kind, err)
	}
	defer func() { _ = f.Close() }()

	var newVersion int64
	lockErr := withFileLock(f, func() error {
		cur, scanErr := latestVersionLocked(f, id)
		if scanErr != nil {
			return scanErr
		}
		if cur != expectedVersion {
			return fmt.Errorf("%w: %s expected v%d but current is v%d", ErrVersionConflict, id, expectedVersion, cur)
		}
		newVersion = cur + 1
		payload["version"] = newVersion
		e := Entry{
			UUID:      id,
			Kind:      kind,
			CreatedAt: time.Now().UTC().Format(time.RFC3339),
			Author:    author,
			Payload:   payload,
		}
		data, mErr := json.Marshal(e)
		if mErr != nil {
			return fmt.Errorf("brain: marshal entry: %w", mErr)
		}
		if _, wErr := fmt.Fprintf(f, "%s\n", data); wErr != nil {
			return fmt.Errorf("brain: write %s: %w", kind, wErr)
		}
		return nil
	})
	if lockErr != nil {
		return 0, lockErr
	}
	return newVersion, nil
}

// latestVersionLocked scans the open (and already flock-held) file from the
// start and returns the version of the last entry matching id (0 if absent).
// The file is opened O_APPEND so the subsequent write still lands at EOF.
func latestVersionLocked(f *os.File, id string) (int64, error) {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return 0, fmt.Errorf("brain: seek: %w", err)
	}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	var version int64
	for sc.Scan() {
		e, parseErr := parseEntry(sc.Bytes())
		if parseErr != nil {
			continue
		}
		if e.UUID == id {
			version = entryVersion(e)
		}
	}
	if err := sc.Err(); err != nil {
		return 0, fmt.Errorf("brain: scan: %w", err)
	}
	return version, nil
}
