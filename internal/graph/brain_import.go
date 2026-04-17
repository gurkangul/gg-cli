package graph

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
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

// ImportChunkResult maps the old exported node ID to the new Memgraph element ID
// assigned on this machine after MERGE.
type ImportChunkResult struct {
	// OldToNew maps old element ID (from chunks.jsonl) → new element ID.
	OldToNew map[string]string
	// Imported is the number of nodes successfully upserted.
	Imported int
	// Skipped is the number of nodes that could not be upserted (missing merge key).
	Skipped int
}

// ImportChunks reads chunks.jsonl and upserts each node into Memgraph, returning
// an old→new element ID mapping for edge resolution.
//
// project_id in each node's properties is rewritten to c.projectID.
func (c *Client) ImportChunks(ctx context.Context, chunksPath string) (*ImportChunkResult, error) {
	nodes, err := readChunksJSONL(chunksPath)
	if err != nil {
		return nil, err
	}

	result := &ImportChunkResult{
		OldToNew: make(map[string]string, len(nodes)),
	}

	for _, raw := range nodes {
		oldID := raw.ID
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
			return nil, fmt.Errorf("import node %s (label=%s): %w", oldID, label, err)
		}
		if n.ID != "" {
			result.OldToNew[oldID] = n.ID
		}
		result.Imported++
	}
	return result, nil
}

// ImportEdges reads edges.jsonl and upserts each relationship into Memgraph.
// oldToNew maps old element IDs (from chunks.jsonl export) to new IDs on this machine.
// Edges whose src or dst is not in oldToNew are skipped.
func (c *Client) ImportEdges(ctx context.Context, edgesPath string, oldToNew map[string]string) (int, int, error) {
	edges, err := readEdgesJSONL(edgesPath)
	if err != nil {
		return 0, 0, err
	}

	imported, skipped := 0, 0
	for _, raw := range edges {
		newSrc, srcOK := oldToNew[raw.Src]
		newDst, dstOK := oldToNew[raw.Dst]
		if !srcOK || !dstOK {
			skipped++
			continue
		}
		if raw.Type == "" {
			skipped++
			continue
		}

		props := make(map[string]any, len(raw.Properties))
		for k, v := range raw.Properties {
			if k != "project_id" {
				props[k] = v
			}
		}

		e := &Edge{
			FromID:     newSrc,
			ToID:       newDst,
			Type:       raw.Type,
			Properties: props,
		}
		if err := c.UpsertEdge(ctx, e); err != nil {
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

func readJSONL[T any](path string) ([]T, error) {
	f, err := os.Open(path) //nolint:gosec
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	var records []T
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1<<20), 1<<20)
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
		return nil, fmt.Errorf("scan %s: %w", path, err)
	}
	return records, nil
}
