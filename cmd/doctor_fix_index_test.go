package cmd

import (
	"testing"
)

func TestShouldUseFullIndexForFix(t *testing.T) {
	cases := []struct {
		name   string
		status codeGraphStatus
		want   bool
	}{
		{"missing graph empty", codeGraphStatus{Status: "missing", GraphEmpty: true}, true},
		{"missing graph unavailable", codeGraphStatus{Status: "missing", GraphDetail: "unavailable"}, true},
		{"missing graph not checked", codeGraphStatus{Status: "missing", GraphDetail: "not checked"}, true},
		{"missing stats unavailable", codeGraphStatus{Status: "missing", GraphAvailable: true, GraphStatsAvailable: false}, true},
		{"missing other", codeGraphStatus{Status: "missing", GraphAvailable: true, GraphStatsAvailable: true, GraphDetail: "reachable"}, false},
		{"stale graph empty", codeGraphStatus{Status: "stale", GraphEmpty: true}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := shouldUseFullIndexForFix(tc.status)
			if got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// NOTE: TestDoctorFixIndex_RepopulatesEmptyMemgraphProjection was removed: it was a
// live-Memgraph integration test (GG_INTEGRATION_TEST + scip-go + bolt://localhost:7687)
// that built a graph client from the deleted config.Config.Memgraph field. The
// Memgraph server backend no longer exists; the embedded graph projection is
// covered by the graph package's embedded-store tests.
