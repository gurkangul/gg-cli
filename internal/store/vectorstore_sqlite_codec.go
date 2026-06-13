package store

import (
	"encoding/binary"
	"fmt"
	"math"

	"github.com/qdrant/go-client/qdrant"
	"google.golang.org/protobuf/proto"
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

// --- payload codec (lossless via the qdrant.Struct proto message) ---

func encodePayload(payload map[string]*qdrant.Value) ([]byte, error) {
	if payload == nil {
		payload = map[string]*qdrant.Value{}
	}
	return proto.Marshal(&qdrant.Struct{Fields: payload})
}

func decodePayload(buf []byte) (map[string]*qdrant.Value, error) {
	var s qdrant.Struct
	if err := proto.Unmarshal(buf, &s); err != nil {
		return nil, fmt.Errorf("decode payload: %w", err)
	}
	if s.GetFields() == nil {
		return map[string]*qdrant.Value{}, nil
	}
	return s.GetFields(), nil
}

// outputVectors wraps a dense float32 slice in the VectorsOutput shape a
// RetrievedPoint/ScoredPoint carries. It populates the Dense field (current
// protocol) so extractVector and callers reading GetVectors() round-trip the
// vector identically to Qdrant. Returns nil for an empty vector.
func outputVectors(vec []float32) *qdrant.VectorsOutput {
	if len(vec) == 0 {
		return nil
	}
	return &qdrant.VectorsOutput{
		VectorsOptions: &qdrant.VectorsOutput_Vector{
			Vector: &qdrant.VectorOutput{
				Vector: &qdrant.VectorOutput_Dense{Dense: &qdrant.DenseVector{Data: vec}},
			},
		},
	}
}

// wantsVectors reports whether a WithVectorsSelector requests vector inclusion.
func wantsVectors(sel *qdrant.WithVectorsSelector) bool {
	if sel == nil {
		return false
	}
	switch opt := sel.GetSelectorOptions().(type) {
	case *qdrant.WithVectorsSelector_Enable:
		return opt.Enable
	case *qdrant.WithVectorsSelector_Include:
		return len(opt.Include.GetNames()) > 0
	default:
		return false
	}
}

// --- cosine similarity (Distance_Cosine parity) ---

// cosineSimilarity returns the cosine similarity between a and b. It matches
// Qdrant's Cosine distance scoring: dot(a,b) / (|a| * |b|), in [-1, 1] with
// 1.0 the closest match. A zero-magnitude vector yields 0 (degraded records are
// filtered out before scoring, but this keeps the math defined).
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

// projectPayload applies a WithPayloadSelector to a full payload map, mirroring
// Qdrant's behavior:
//   - nil selector or Enable(false): return nil (no payload).
//   - Enable(true): return the full payload.
//   - Include(fields...): return only the listed fields that are present.
//   - Exclude(fields...): return everything except the listed fields.
func projectPayload(full map[string]*qdrant.Value, sel *qdrant.WithPayloadSelector) map[string]*qdrant.Value {
	if sel == nil {
		return nil
	}
	switch opt := sel.GetSelectorOptions().(type) {
	case *qdrant.WithPayloadSelector_Enable:
		if opt.Enable {
			return full
		}
		return nil
	case *qdrant.WithPayloadSelector_Include:
		out := make(map[string]*qdrant.Value)
		for _, f := range opt.Include.GetFields() {
			if v, ok := full[f]; ok {
				out[f] = v
			}
		}
		return out
	case *qdrant.WithPayloadSelector_Exclude:
		excl := make(map[string]struct{}, len(opt.Exclude.GetFields()))
		for _, f := range opt.Exclude.GetFields() {
			excl[f] = struct{}{}
		}
		out := make(map[string]*qdrant.Value, len(full))
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
