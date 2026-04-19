package store

import (
	"testing"
)

func TestPercentile95(t *testing.T) {
	cases := []struct {
		sorted []int
		want   int
	}{
		{[]int{}, 0},
		{[]int{5}, 5},
		// n=10: rank = ceil(0.95*10) = ceil(9.5) = 10 → sorted[9] = 10
		{[]int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}, 10},
		{[]int{1, 1, 1, 1, 1, 1, 1, 1, 1, 10}, 10},
		{[]int{2, 3, 3, 3, 3, 3, 3, 3, 3, 4}, 4},
		// n=20: rank = ceil(0.95*20) = 19 → sorted[18] = 9
		{[]int{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 9, 10}, 9},
	}
	for _, c := range cases {
		got := percentile95(c.sorted)
		if got != c.want {
			t.Errorf("percentile95(%v) = %d, want %d", c.sorted, got, c.want)
		}
	}
}

func TestSurfacePressureStats_Empty(t *testing.T) {
	// Verify zero-value stats for empty sample.
	var sp SurfacePressureStats
	if sp.SampleSize != 0 {
		t.Errorf("expected zero SampleSize, got %d", sp.SampleSize)
	}
	if sp.P95FilesPerFix != 0 {
		t.Errorf("expected zero P95FilesPerFix, got %d", sp.P95FilesPerFix)
	}
}
