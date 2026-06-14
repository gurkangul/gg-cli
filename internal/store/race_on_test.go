//go:build race

package store

// raceEnabled is true when the test binary is built with -race. The latency
// gate in TestSQLiteVec_QueryLatencyAtScale skips under -race because the
// detector's ~10-20x memory-access overhead makes wall-clock timing
// meaningless; brute-force correctness is still covered by the other tests.
const raceEnabled = true
