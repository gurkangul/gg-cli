package store

import (
	"context"
	"fmt"
	"time"
)

// Bundle is the portable export format for a gg project.
// It captures all vector-store collection data for one project and can be imported
// into a different machine or project ID without re-embedding.
//
// Note: Memgraph graph data (gg index) is not included — it is derived from
// source code and can be rebuilt by running 'gg index' after import.
type Bundle struct {
	Version         string                   `json:"version"`
	ExportedAt      string                   `json:"exported_at"`
	SourceProjectID string                   `json:"source_project_id"`
	Collections     map[string][]BundlePoint `json:"collections"`
}

// BundlePoint is a single serialized stored point (id + payload + vector).
type BundlePoint struct {
	ID      string         `json:"id"`
	Payload map[string]any `json:"payload"`
	Vector  []float32      `json:"vector,omitempty"`
}

// ExportBundle scrolls all project collections and serializes them into a Bundle.
// Vectors are included so that import does not require re-embedding.
func (c *Client) ExportBundle(ctx context.Context) (*Bundle, error) {
	kinds := []string{
		collSuffixDecisions, collSuffixTasks, collSuffixMessages,
		collSuffixRejections, collSuffixDiscussions, collSuffixNotes, collSuffixBugs,
	}

	bundle := &Bundle{
		Version:         "1",
		ExportedAt:      time.Now().UTC().Format(time.RFC3339),
		SourceProjectID: c.projectID,
		Collections:     make(map[string][]BundlePoint, len(kinds)),
	}

	for _, kind := range kinds {
		coll := c.projectID + "-" + kind
		points, err := c.scrollWithVectors(ctx, coll)
		if err != nil {
			return nil, fmt.Errorf("export %s: %w", kind, err)
		}
		bundle.Collections[kind] = points
	}
	return bundle, nil
}

// bundleVectorDim returns the embedding dimension of the first non-empty vector
// in the bundle, or 0 when the bundle carries no vectors. All points in a bundle
// share one embedding model, so the first vector's length is authoritative.
func bundleVectorDim(bundle *Bundle) int {
	for _, points := range bundle.Collections {
		for _, p := range points {
			if len(p.Vector) > 0 {
				return len(p.Vector)
			}
		}
	}
	return 0
}

// ImportBundle upserts all points from bundle into this project's collections.
// Vectors are reused as-is — no re-embedding required.
// Existing points with the same ID are overwritten (upsert semantics).
func (c *Client) ImportBundle(ctx context.Context, bundle *Bundle) error {
	// Size collections to the bundle's actual vector dimension, not a hardcoded
	// default: a bundle exported from a non-768 model (e.g. qwen3-embedding=1024)
	// must not be loaded into 768-sized collections, which would silently store
	// mismatched-length vectors and break recall. An empty/vectorless bundle falls
	// back to the nomic default.
	dim := bundleVectorDim(bundle)
	if dim == 0 {
		dim = VectorSize
	}
	if err := c.EnsureCollections(ctx, uint64(dim)); err != nil {
		return fmt.Errorf("ensure collections: %w", err)
	}

	for kind, points := range bundle.Collections {
		if len(points) == 0 {
			continue
		}
		coll := c.projectID + "-" + kind
		if err := c.upsertBundlePoints(ctx, coll, points); err != nil {
			return fmt.Errorf("import %s: %w", kind, err)
		}
	}
	return nil
}

// scrollWithVectors pages through all points in coll, returning payload and vector for each.
func (c *Client) scrollWithVectors(ctx context.Context, coll string) ([]BundlePoint, error) {
	pageSize := uint32(256)
	withVecs := NewWithVectorsEnable(true)
	withPay := NewWithPayloadEnable(true)

	var all []BundlePoint
	var offset *PointId
	for {
		page, next, err := c.vs.ScrollAndOffset(ctx, &ScrollPoints{
			CollectionName: coll,
			Limit:          &pageSize,
			Offset:         offset,
			WithPayload:    withPay,
			WithVectors:    withVecs,
		})
		if err != nil {
			// Collection missing on fresh install — treat as empty.
			return nil, nil //nolint:nilerr
		}
		for _, p := range page {
			bp := BundlePoint{
				ID:      p.GetId().GetUuid(),
				Payload: extractPayload(p.GetPayload()),
				Vector:  extractVector(p.GetVectors()),
			}
			if bp.ID == "" {
				continue // skip malformed points
			}
			all = append(all, bp)
		}
		if next == nil {
			break
		}
		offset = next
	}
	return all, nil
}

// upsertBundlePoints writes a batch of BundlePoints into the target collection.
func (c *Client) upsertBundlePoints(ctx context.Context, coll string, points []BundlePoint) error {
	const batchSize = 128
	for i := 0; i < len(points); i += batchSize {
		end := i + batchSize
		if end > len(points) {
			end = len(points)
		}
		batch := points[i:end]
		structs := make([]*PointStruct, 0, len(batch))
		for _, bp := range batch {
			payload, err := TryValueMap(bp.Payload)
			if err != nil {
				return fmt.Errorf("build payload for %s: %w", bp.ID, err)
			}
			ps := &PointStruct{
				Id:      NewID(bp.ID),
				Payload: payload,
			}
			if len(bp.Vector) > 0 {
				ps.Vectors = NewVectors(bp.Vector...)
			}
			structs = append(structs, ps)
		}
		wait := true
		if err := c.vsUpsert(ctx, &UpsertPoints{
			CollectionName: coll,
			Wait:           &wait,
			Points:         structs,
		}); err != nil {
			return err
		}
	}
	return nil
}

// extractPayload converts a stored point payload to a plain Go map.
func extractPayload(pay map[string]*Value) map[string]any {
	out := make(map[string]any, len(pay))
	for k, v := range pay {
		out[k] = valueToAny(v)
	}
	return out
}

// valueToAny converts a *Value to the corresponding Go type.
func valueToAny(v *Value) any {
	if v == nil {
		return nil
	}
	switch x := v.GetKind().(type) {
	case *Value_StringValue:
		return x.StringValue
	case *Value_IntegerValue:
		return x.IntegerValue
	case *Value_DoubleValue:
		return x.DoubleValue
	case *Value_BoolValue:
		return x.BoolValue
	case *Value_ListValue:
		if x.ListValue == nil {
			return []any{}
		}
		items := make([]any, len(x.ListValue.GetValues()))
		for i, item := range x.ListValue.GetValues() {
			items[i] = valueToAny(item)
		}
		return items
	case *Value_StructValue:
		if x.StructValue == nil {
			return map[string]any{}
		}
		m := make(map[string]any, len(x.StructValue.GetFields()))
		for k, sv := range x.StructValue.GetFields() {
			m[k] = valueToAny(sv)
		}
		return m
	default:
		return nil
	}
}

// extractVector returns the float32 slice from a VectorsOutput.
// Returns nil if no vector data is present.
func extractVector(vecs *VectorsOutput) []float32 {
	if vecs == nil {
		return nil
	}
	vOutput := vecs.GetVector()
	if vOutput == nil {
		return nil
	}
	// Use the current .Dense field (current vector format).
	if dense := vOutput.GetDense(); dense != nil {
		return dense.GetData()
	}
	//lint:ignore SA1019 GetData: fallback for older vector format versions that predate Dense oneof
	if data := vOutput.GetData(); len(data) > 0 { //nolint:staticcheck // GetData: deprecated flat fallback for pre-Dense-oneof the vector store
		return data
	}
	return nil
}
