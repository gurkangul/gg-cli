//go:build !race

package store

// raceEnabled is false in normal (non -race) builds; the latency gate runs.
const raceEnabled = false
