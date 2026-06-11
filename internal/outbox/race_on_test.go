//go:build race

package outbox_test

// raceEnabled is true when the test binary is built with -race. Used to skip
// wall-clock latency assertions whose thresholds are meaningless under race
// instrumentation overhead.
const raceEnabled = true
