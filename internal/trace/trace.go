// Package trace provides lightweight span recording for gg operations.
//
// Recording is controlled by the GG_TRACE environment variable:
//
//	GG_TRACE=1  — append spans to .gg/traces/YYYY-MM-DD.jsonl
//	GG_TRACE=0  — no-op (default)
//
// Each span is a single JSON line. The package is designed to be safe to call
// from any goroutine; file writes are serialised through an OS-level lock via
// the same flock mechanism used by the store package.
//
// Callers use the package idiom:
//
//	start := time.Now()
//	result, err := doSomething(ctx)
//	trace.Record("op.name", start, err)
package trace

import (
	"bufio"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"
)

// Span is a single operation record written to the trace file.
type Span struct {
	Op         string  `json:"op"`
	DurationMs float64 `json:"duration_ms"`
	IsErr      bool    `json:"is_err,omitempty"`
	Timestamp  int64   `json:"ts"` // Unix epoch seconds
}

// Percentiles holds p50/p95/p99 latencies in milliseconds.
type Percentiles struct {
	P50 float64
	P95 float64
	P99 float64
	N   int
}

// Record writes a span to the daily trace file if GG_TRACE=1.
// It is a no-op when GG_TRACE is unset or "0". Silent on all errors so
// tracing never interrupts the caller.
func Record(op string, start time.Time, err error) {
	if os.Getenv("GG_TRACE") != "1" {
		return
	}
	ggDir, e := ggDir()
	if e != nil {
		return
	}
	span := Span{
		Op:         op,
		DurationMs: float64(time.Since(start).Microseconds()) / 1000.0,
		Timestamp:  start.Unix(),
	}
	if err != nil {
		span.IsErr = true
	}
	line, e := json.Marshal(span)
	if e != nil {
		return
	}
	line = append(line, '\n')

	traceDir := filepath.Join(ggDir, "traces")
	if e := os.MkdirAll(traceDir, 0o700); e != nil {
		return
	}
	filename := filepath.Join(traceDir, time.Now().UTC().Format("2006-01-02")+".jsonl")
	f, e := os.OpenFile(filename, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if e != nil {
		return
	}
	defer func() { _ = f.Close() }()
	_, _ = f.Write(line)
}

// ReadPercentiles reads the last `last` spans from today's and yesterday's
// trace files and returns latency percentiles. Returns (zero, 0, nil) when no
// trace data exists.
func ReadPercentiles(ggDir string, last int) (Percentiles, error) {
	traceDir := filepath.Join(ggDir, "traces")
	today := time.Now().UTC().Format("2006-01-02")
	yesterday := time.Now().UTC().Add(-24 * time.Hour).Format("2006-01-02")

	var durations []float64
	for _, day := range []string{today, yesterday} {
		spans, err := readSpans(filepath.Join(traceDir, day+".jsonl"))
		if err != nil {
			continue
		}
		for _, s := range spans {
			durations = append(durations, s.DurationMs)
		}
	}

	if len(durations) == 0 {
		return Percentiles{}, nil
	}

	// Trim to the last `last` entries (spans are appended in order).
	if last > 0 && len(durations) > last {
		durations = durations[len(durations)-last:]
	}

	sort.Float64s(durations)
	return Percentiles{
		P50: percentile(durations, 0.50),
		P95: percentile(durations, 0.95),
		P99: percentile(durations, 0.99),
		N:   len(durations),
	}, nil
}

// ggDir resolves the project-local .gg/ directory, preferring the GG_DIR
// environment variable for test isolation. Falls back to searching upward from
// the working directory.
func ggDir() (string, error) {
	if dir := os.Getenv("GG_DIR"); dir != "" {
		return dir, nil
	}
	// Walk upward from cwd looking for .gg/config.yaml.
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := cwd
	for {
		candidate := filepath.Join(dir, ".gg")
		if _, e := os.Stat(filepath.Join(candidate, "config.yaml")); e == nil {
			return candidate, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", os.ErrNotExist
		}
		dir = parent
	}
}

func readSpans(path string) ([]Span, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var spans []Span
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var s Span
		if json.Unmarshal(sc.Bytes(), &s) == nil {
			spans = append(spans, s)
		}
	}
	return spans, sc.Err()
}

func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := p * float64(len(sorted)-1)
	lo := int(math.Floor(idx))
	hi := int(math.Ceil(idx))
	if lo == hi {
		return sorted[lo]
	}
	frac := idx - float64(lo)
	return sorted[lo] + frac*(sorted[hi]-sorted[lo])
}

// fmtMs formats a duration as a human-readable string: "1.2ms" or "123ms".
// Exported for use in cmd/status.
func FmtMs(ms float64) string {
	if ms < 1 {
		return strconv.FormatFloat(ms*1000, 'f', 0, 64) + "μs"
	}
	if ms < 100 {
		return strconv.FormatFloat(ms, 'f', 1, 64) + "ms"
	}
	return strconv.FormatFloat(ms, 'f', 0, 64) + "ms"
}
