package store

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
)

// --- vector BLOB codec (little-endian float32) ---

func encodeVector(vec []float32) []byte {
	buf := make([]byte, 4*len(vec))
	for i, f := range vec {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(f))
	}
	return buf
}

func decodeVector(buf []byte) []float32 {
	n := len(buf) / 4
	out := make([]float32, n)
	for i := 0; i < n; i++ {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(buf[i*4:]))
	}
	return out
}

// --- payload codec (JSON; native Value <-> plain Go round-trip) ---
//
// The payload is marshalled as a plain JSON object of each field's underlying Go
// value. JSON is lossless for this store because timestamps are RFC3339 strings
// and the only integer payloads are small counters well under 2^53.
//
// JSON decodes every number to float64, so on decode every float64 with no
// fractional part is coerced back to int64 (mirroring
// internal/graph normalizeProps) so integer fields like task_version round-trip
// as int64 rather than float64. The coercion recurses into nested lists/maps.

func encodePayload(payload map[string]*Value) ([]byte, error) {
	plain := make(map[string]any, len(payload))
	for k, v := range payload {
		plain[k] = valueUnderlying(v)
	}
	return json.Marshal(plain)
}

func decodePayload(buf []byte) (map[string]*Value, error) {
	if len(buf) == 0 {
		return map[string]*Value{}, nil
	}
	var plain map[string]any
	if err := json.Unmarshal(buf, &plain); err != nil {
		return nil, fmt.Errorf("decode payload: %w", err)
	}
	out := make(map[string]*Value, len(plain))
	for k, raw := range plain {
		v, err := NewValue(coerceIntegral(raw))
		if err != nil {
			return nil, fmt.Errorf("decode payload field %q: %w", k, err)
		}
		out[k] = v
	}
	return out, nil
}

// coerceIntegral recursively coerces any float64 with no fractional part to
// int64 so integer payload fields survive the JSON round-trip as int64. Strings,
// bools and nil pass through; lists and maps recurse.
func coerceIntegral(v any) any {
	switch x := v.(type) {
	case float64:
		if x == float64(int64(x)) && !math.IsInf(x, 0) {
			return int64(x)
		}
		return x
	case []any:
		for i := range x {
			x[i] = coerceIntegral(x[i])
		}
		return x
	case map[string]any:
		for k := range x {
			x[k] = coerceIntegral(x[k])
		}
		return x
	default:
		return v
	}
}

// outputVectors wraps a dense float32 slice in the VectorsOutput shape a
// RetrievedPoint/ScoredPoint carries. It populates the Dense field so callers
// reading GetVectors() round-trip the vector identically. Returns nil for an
// empty vector.
func outputVectors(vec []float32) *VectorsOutput {
	if len(vec) == 0 {
		return nil
	}
	return &VectorsOutput{
		VectorsOptions: &VectorsOutput_Vector{
			Vector: &VectorOutput{
				Vector: &VectorOutput_Dense{Dense: &DenseVector{Data: vec}},
			},
		},
	}
}

// wantsVectors reports whether a WithVectorsSelector requests vector inclusion.
func wantsVectors(sel *WithVectorsSelector) bool {
	if sel == nil {
		return false
	}
	switch opt := sel.GetSelectorOptions().(type) {
	case *WithVectorsSelector_Enable:
		return opt.Enable
	case *WithVectorsSelector_Include:
		return len(opt.Include.GetNames()) > 0
	default:
		return false
	}
}

// --- cosine similarity (Cosine-distance parity) ---

// cosineSimilarity returns the cosine similarity between a and b: dot(a,b) /
// (|a| * |b|), in [-1, 1] with 1.0 the closest match. A zero-magnitude vector
// yields 0 (degraded records are filtered out before scoring, but this keeps the
// math defined).
func cosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		av := float64(a[i])
		bv := float64(b[i])
		dot += av * bv
		na += av * av
		nb += bv * bv
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return float32(dot / (math.Sqrt(na) * math.Sqrt(nb)))
}

// --- payload projection (WithPayloadSelector parity) ---

// projectPayload applies a WithPayloadSelector to a full payload map:
//   - nil selector or Enable(false): return nil (no payload).
//   - Enable(true): return the full payload.
//   - Include(fields...): return only the listed fields that are present.
//   - Exclude(fields...): return everything except the listed fields.
func projectPayload(full map[string]*Value, sel *WithPayloadSelector) map[string]*Value {
	if sel == nil {
		return nil
	}
	switch opt := sel.GetSelectorOptions().(type) {
	case *WithPayloadSelector_Enable:
		if opt.Enable {
			return full
		}
		return nil
	case *WithPayloadSelector_Include:
		out := make(map[string]*Value)
		for _, f := range opt.Include.GetFields() {
			if v, ok := full[f]; ok {
				out[f] = v
			}
		}
		return out
	case *WithPayloadSelector_Exclude:
		excl := make(map[string]struct{}, len(opt.Exclude.GetFields()))
		for _, f := range opt.Exclude.GetFields() {
			excl[f] = struct{}{}
		}
		out := make(map[string]*Value, len(full))
		for k, v := range full {
			if _, drop := excl[k]; !drop {
				out[k] = v
			}
		}
		return out
	default:
		return nil
	}
}
