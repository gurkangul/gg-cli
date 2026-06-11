//go:build !race

package outbox_test

// raceEnabled is false in normal (non -race) builds.
const raceEnabled = false
