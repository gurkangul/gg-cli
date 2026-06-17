package store

import "testing"

// TestIsZeroVector_DetectsPlaceholder guards BUG-009: brain import must seed
// zero-vector placeholders so EmbedMissing can detect and re-embed them.
// Before the fix, UpsertBrainRecords did not seed a vector, causing the fresh
// vector-store collection to reject points (vector is mandatory).
func TestIsZeroVector_DetectsPlaceholder(t *testing.T) {
	cases := []struct {
		name string
		v    []float32
		want bool
	}{
		{"all zeros", []float32{0, 0, 0}, true},
		{"single zero", []float32{0}, true},
		{"empty", []float32{}, true},
		{"non-zero", []float32{0, 0, 1}, false},
		{"all non-zero", []float32{1, 2, 3}, false},
		{"mixed", []float32{0, 0.001, 0}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := isZeroVector(tc.v)
			if got != tc.want {
				t.Errorf("isZeroVector(%v) = %v; want %v", tc.v, got, tc.want)
			}
		})
	}
}
