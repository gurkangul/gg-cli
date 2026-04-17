package store

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/qdrant/go-client/qdrant"
)

// brainScanMaxBytes is the per-line limit for the JSONL scanner.
// Declared as a variable (not a constant) so package-level tests can reduce
// it without allocating 65 MB to exercise the oversize error path.
var brainScanMaxBytes = 64 << 20 // 64 MB

// UpsertBrainRecords writes a slice of BrainRecords into the named collection
// (by kind suffix). Vectors are not set — the points are imported payload-only.
// Idempotent: re-running with the same records overwrites existing points by UUID.
func (c *Client) UpsertBrainRecords(ctx context.Context, kind string, records []BrainRecord) error {
	if len(records) == 0 {
		return nil
	}
	coll := c.projectID + "-" + kind

	const batchSize = 128
	for i := 0; i < len(records); i += batchSize {
		end := i + batchSize
		if end > len(records) {
			end = len(records)
		}
		batch := records[i:end]
		structs := make([]*qdrant.PointStruct, 0, len(batch))
		for _, r := range batch {
			payload, err := qdrant.TryValueMap(r.Payload)
			if err != nil {
				return fmt.Errorf("build payload for %s: %w", r.ID, err)
			}
			structs = append(structs, &qdrant.PointStruct{
				Id:      qdrant.NewID(r.ID),
				Payload: payload,
				// No vector — caller must run 'gg reindex --embed' to populate.
			})
		}
		wait := true
		if err := c.qdrantUpsert(ctx, &qdrant.UpsertPoints{
			CollectionName: coll,
			Wait:           &wait,
			Points:         structs,
		}); err != nil {
			return fmt.Errorf("upsert batch %s[%d:%d]: %w", kind, i, end, err)
		}
	}
	return nil
}

// ReadBrainJSONL reads a JSONL file and returns BrainRecord slices.
// Each line must be a JSON object with "id" and "payload" fields.
func ReadBrainJSONL(path string) ([]BrainRecord, error) {
	f, err := os.Open(path) //nolint:gosec
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	var records []BrainRecord
	scanner := bufio.NewScanner(f)
	initBuf := 64 << 10
	if initBuf > brainScanMaxBytes {
		initBuf = brainScanMaxBytes
	}
	scanner.Buffer(make([]byte, initBuf), brainScanMaxBytes)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var r BrainRecord
		if err := json.Unmarshal(line, &r); err != nil {
			return nil, fmt.Errorf("%s line %d: %w", path, lineNum, err)
		}
		if r.ID == "" {
			return nil, fmt.Errorf("%s line %d: missing id", path, lineNum)
		}
		records = append(records, r)
	}
	if err := scanner.Err(); err != nil {
		if errors.Is(err, bufio.ErrTooLong) {
			return nil, fmt.Errorf("scan %s line %d: exceeds 64 MB — payload too large for current buffer", path, lineNum+1)
		}
		return nil, fmt.Errorf("scan %s: %w", path, err)
	}
	return records, nil
}
