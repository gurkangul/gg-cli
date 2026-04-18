package graph

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

// mergeKeysForLabel returns the property keys used to MERGE a node of the
// given label. These match the keys used by the index runner (cmd/index.go).
func mergeKeysForLabel(label string) []string {
	switch label {
	case LabelFile:
		return []string{"path"}
	case LabelSymbol:
		return []string{"name", "source_file"}
	case LabelPackage:
		return []string{"import_path"}
	default:
		// Unknown label: merge on "name" if present, otherwise "id" is a
		// fallback that produces stable but non-semantic merges.
		return []string{"name"}
	}
}

// ImportChunkResult summarises the result of ImportChunks.
type ImportChunkResult struct {
	// Imported is the number of nodes successfully upserted.
	Imported int
	// Skipped is the number of nodes that could not be upserted (missing merge key).
	Skipped int
}

// parseDomainKey is the inverse of stableDomainKey. It decodes a domain-key
// string back into the node label and the merge-key property map needed to
// MATCH or MERGE the node in Memgraph.
//
// Returns (label, mergeProps, true) on success, or ("", nil, false) if the key
// is unrecognised or malformed.
func parseDomainKey(key string) (label string, mergeProps map[string]any, ok bool) {
	if s, found := strings.CutPrefix(key, "file:"); found {
		return LabelFile, map[string]any{"path": s}, true
	}
	if s, found := strings.CutPrefix(key, "symbol:"); found {
		// format: <source_file>#<name>  (use LastIndex so "#" in filenames works)
		idx := strings.LastIndex(s, "#")
		if idx < 0 || idx == len(s)-1 {
			return "", nil, false
		}
		return LabelSymbol, map[string]any{
			"source_file": s[:idx],
			"name":        s[idx+1:],
		}, true
	}
	if s, found := strings.CutPrefix(key, "package:"); found {
		return LabelPackage, map[string]any{"import_path": s}, true
	}
	if s, found := strings.CutPrefix(key, "node:"); found {
		// format: <label>:<name>
		idx := strings.Index(s, ":")
		if idx < 0 || idx == len(s)-1 {
			return "", nil, false
		}
		return s[:idx], map[string]any{"name": s[idx+1:]}, true
	}
	return "", nil, false
}

// ImportChunks reads chunks.jsonl and upserts each node into Memgraph.
// project_id in each node's properties is rewritten to c.projectID.
// The exported node IDs are stable domain keys (e.g. "file:path/to/file.go");
// no old→new element-ID translation is required.
func (c *Client) ImportChunks(ctx context.Context, chunksPath string) (*ImportChunkResult, error) {
	nodes, err := readChunksJSONL(chunksPath)
	if err != nil {
		return nil, err
	}

	result := &ImportChunkResult{}

	for _, raw := range nodes {
		label := raw.Label
		if label == "" {
			result.Skipped++
			continue
		}

		props := make(map[string]any, len(raw.Properties))
		for k, v := range raw.Properties {
			if k != "project_id" {
				props[k] = v
			}
		}

		mergeKeys := mergeKeysForLabel(label)

		// Verify that all merge keys are present; skip if not.
		allPresent := true
		for _, k := range mergeKeys {
			if _, ok := props[k]; !ok {
				allPresent = false
				break
			}
		}
		if !allPresent {
			result.Skipped++
			continue
		}

		n := &Node{Label: label, Properties: props}
		if err := c.UpsertNode(ctx, n, mergeKeys); err != nil {
			return nil, fmt.Errorf("import node %s (label=%s): %w", raw.ID, label, err)
		}
		result.Imported++
	}
	return result, nil
}

// ImportEdges reads edges.jsonl and upserts each relationship into Memgraph.
// src/dst values in edges.jsonl are stable domain keys (e.g. "file:…",
// "symbol:…") produced by ExportEdges — no element-ID translation is needed.
// Edges whose src or dst cannot be parsed as a domain key are skipped.
func (c *Client) ImportEdges(ctx context.Context, edgesPath string) (int, int, error) {
	edges, err := readEdgesJSONL(edgesPath)
	if err != nil {
		return 0, 0, err
	}

	imported, skipped := 0, 0
	for _, raw := range edges {
		if raw.Type == "" {
			skipped++
			continue
		}

		srcLabel, srcMerge, srcOK := parseDomainKey(raw.Src)
		dstLabel, dstMerge, dstOK := parseDomainKey(raw.Dst)
		if !srcOK || !dstOK {
			skipped++
			continue
		}

		props := make(map[string]any, len(raw.Properties))
		for k, v := range raw.Properties {
			if k != "project_id" {
				props[k] = v
			}
		}

		if err := c.UpsertEdgeByKey(ctx, srcLabel, srcMerge, dstLabel, dstMerge, raw.Type, props); err != nil {
			return imported, skipped, fmt.Errorf("import edge %s→%s type=%s: %w", raw.Src, raw.Dst, raw.Type, err)
		}
		imported++
	}
	return imported, skipped, nil
}

// rawChunkRecord is the on-disk shape of a chunks.jsonl line.
type rawChunkRecord struct {
	ID         string         `json:"id"`
	Label      string         `json:"label"`
	Properties map[string]any `json:"properties"`
}

// rawEdgeRecord is the on-disk shape of an edges.jsonl line.
type rawEdgeRecord struct {
	Dst        string         `json:"dst"`
	Properties map[string]any `json:"properties"`
	Src        string         `json:"src"`
	Type       string         `json:"type"`
}

func readChunksJSONL(path string) ([]rawChunkRecord, error) {
	return readJSONL[rawChunkRecord](path)
}

func readEdgesJSONL(path string) ([]rawEdgeRecord, error) {
	return readJSONL[rawEdgeRecord](path)
}

// jsonlMaxLineBytes is the maximum size of a single JSONL line accepted by readJSONL.
// Large symbol graphs can produce wide lines (many properties), so we allow 64 MiB.
// Raising this is safe: it only allocates on demand when lines are long.
// Declared as a variable (not a constant) so package-level tests can reduce it
// without allocating 65 MiB to exercise the oversize error path.
var jsonlMaxLineBytes = 64 << 20 // 64 MiB

func readJSONL[T any](path string) ([]T, error) {
	f, err := os.Open(path) //nolint:gosec
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	var records []T
	scanner := bufio.NewScanner(f)
	initBuf := 4 << 10
	if initBuf > jsonlMaxLineBytes {
		initBuf = jsonlMaxLineBytes
	}
	scanner.Buffer(make([]byte, initBuf), jsonlMaxLineBytes)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var r T
		if err := json.Unmarshal(line, &r); err != nil {
			return nil, fmt.Errorf("%s line %d: %w", path, lineNum, err)
		}
		records = append(records, r)
	}
	if err := scanner.Err(); err != nil {
		if errors.Is(err, bufio.ErrTooLong) {
			return nil, fmt.Errorf("%s: JSONL line exceeds %d MiB limit — snapshot may be corrupt; re-run 'gg brain export'", path, jsonlMaxLineBytes>>20)
		}
		return nil, fmt.Errorf("scan %s: %w", path, err)
	}
	return records, nil
}
