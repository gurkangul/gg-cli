package brain

// Fold semantics (BUG-062 / BUG-063 / BUG-069):
//
// JSONL is the durable source of truth. Create paths append a record once;
// every subsequent mutation appends a NEW line carrying the SAME uuid and the
// FULL updated payload. Because Append is ordered and flock-protected, the file
// order is chronological, so the LAST line for a uuid is the current state.
//
// FoldLatest collapses a raw entry slice (as returned by ReadAll) to one entry
// per uuid — the latest — implementing last-write-wins. Every reader that needs
// current state (reconcile, reembed, offline reads) folds before use; the old
// first-write-wins dedup silently reverted mutations on any Qdrant rebuild.

// FoldLatest returns one Entry per UUID, keeping the last occurrence in the
// input slice (last-write-wins). Output order follows the position of each
// uuid's last occurrence, so the result stays deterministic and chronological.
func FoldLatest(entries []Entry) []Entry {
	if len(entries) == 0 {
		return nil
	}
	latestIdx := make(map[string]int, len(entries))
	for i, e := range entries {
		latestIdx[e.UUID] = i
	}
	out := make([]Entry, 0, len(latestIdx))
	for i, e := range entries {
		if latestIdx[e.UUID] == i {
			out = append(out, e)
		}
	}
	return out
}

// ReadLatest reads .gg/brain/<kind>.jsonl and folds it to current state
// (one entry per uuid, last-write-wins). It is the durable-source-of-truth
// view callers should use when they need each record's current payload rather
// than its full mutation history.
func ReadLatest(ggDir, kind string) ([]Entry, error) {
	entries, err := ReadAll(ggDir, kind)
	if err != nil {
		return nil, err
	}
	return FoldLatest(entries), nil
}

// ReadLatestWithCount is ReadLatest plus the malformed-line count from the
// underlying scan, so callers can surface JSONL corruption (BUG-070).
func ReadLatestWithCount(ggDir, kind string) (entries []Entry, skipped int, err error) {
	all, skipped, err := ReadAllWithCount(ggDir, kind)
	if err != nil {
		return nil, skipped, err
	}
	return FoldLatest(all), skipped, nil
}
