package telemetry

import (
	"bufio"
	"math"
	"os"
	"strings"
	"testing"
)

func TestCountTokens(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int
	}{
		{"empty", "", 0},
		{"single word", "hello", 1},
		{"two words", "hello world", 2},
		{"camel case", "helloWorld", 1}, // single alnum run
		{"with digits", "TASK-285", 3},  // "TASK" + "-" + "285"
		// ○=1 TASK=1 -=1 285=1 [=1 low=1 ]=1 Compact=1 :=1 token=1 tahmini=1 → 11
		{"gg compact line", "○ TASK-285 [low] Compact: token tahmini", 11},
		{"punctuation only", ".,;:!?", 6},
		{"newlines ignored", "a\nb\nc", 3},
		{"tabs ignored", "a\tb\tc", 3},
		{"turkish mixed", "tahmini kalibrasyon", 2},
		{"unicode glyph", "✓ done", 2},  // "✓" = 1 token + "done" = 1 token
		{"all spaces", "   \t\n  ", 0},
		{"id plus brackets", "[low]", 3}, // "[" + "low" + "]" = 3
		{"sha", "a30c6b3", 1},            // alnum run
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CountTokens(tt.input)
			if got != tt.want {
				t.Errorf("CountTokens(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestBytesPerTokenFor(t *testing.T) {
	t.Run("empty string returns 0", func(t *testing.T) {
		if got := BytesPerTokenFor(""); got != 0 {
			t.Errorf("BytesPerTokenFor(\"\") = %d, want 0", got)
		}
	})

	t.Run("known compact corpus pins near 4 bytes per token", func(t *testing.T) {
		// Pinned from corpus measurement: 25-line golden corpus → 4.024 bytes/token → rounded=4.
		// Epsilon ±0.5 allows minor corpus drift; re-run TestEmitCalibrationFactor if it breaks.
		want := 4.0
		epsilon := 0.5
		samples := loadCorpusGolden(t)
		r := CalibrateCorpus(samples)
		if r.TotalTokens == 0 {
			t.Fatal("corpus returned 0 tokens")
		}
		if r.Ratio < want-epsilon || r.Ratio > want+epsilon {
			t.Errorf("corpus ratio = %.3f, want %.1f ±%.1f — re-run TestEmitCalibrationFactor to recalibrate", r.Ratio, want, epsilon)
		}
	})

	t.Run("word only has ratio ≥ 1", func(t *testing.T) {
		if got := BytesPerTokenFor("hello"); got < 1 {
			t.Errorf("BytesPerTokenFor(\"hello\") = %d, want ≥1", got)
		}
	})
}

func TestCalibrateCorpus(t *testing.T) {
	t.Run("empty corpus", func(t *testing.T) {
		r := CalibrateCorpus(nil)
		if r.TotalTokens != 0 || r.Rounded != 0 {
			t.Errorf("empty corpus: got %+v, want zero result", r)
		}
	})

	t.Run("all empty strings", func(t *testing.T) {
		r := CalibrateCorpus([]string{"", "", ""})
		if r.TotalTokens != 0 {
			t.Errorf("all-empty: TotalTokens = %d, want 0", r.TotalTokens)
		}
	})

	t.Run("known corpus gives BytesPerToken≈3", func(t *testing.T) {
		// These are representative lines from gg compact output.
		samples := []string{
			"○ TASK-285 [low] Compact: token tahmini disclaimer",
			"○ TASK-276 [high] fix parallel mutex + cross-process flock",
			"D  2026-04-24  master resume protocol — 7-command pipeline",
			"✓ done  39dd7ab  fix(parallel): drain-loop reason",
			"Compact  426 calls, 1.2 MB / ~327K tok saved (avg 65% reduction)",
		}
		r := CalibrateCorpus(samples)
		if r.TotalTokens == 0 {
			t.Fatal("expected non-zero token count")
		}
		// Ratio must be plausible (1..6 bytes/token for mixed ASCII+glyphs).
		if r.Ratio < 1.0 || r.Ratio > 6.0 {
			t.Errorf("Ratio = %.2f, want in [1,6]", r.Ratio)
		}
		// Rounded must match floor(ratio + 0.5).
		wantRounded := int(math.Round(r.Ratio))
		if wantRounded < 1 {
			wantRounded = 1
		}
		if r.Rounded != wantRounded {
			t.Errorf("Rounded = %d, want %d (round(%.2f))", r.Rounded, wantRounded, r.Ratio)
		}
	})

	t.Run("single-char corpus", func(t *testing.T) {
		r := CalibrateCorpus([]string{"a"})
		if r.TotalBytes != 1 || r.TotalTokens != 1 {
			t.Errorf("single char: bytes=%d tokens=%d, want 1/1", r.TotalBytes, r.TotalTokens)
		}
		if r.Rounded < 1 {
			t.Errorf("Rounded = %d, want ≥1", r.Rounded)
		}
	})
}

// loadCorpusGolden reads testdata/compact_corpus.golden and returns non-blank,
// non-comment lines as the calibration corpus. Comments start with '#'.
func loadCorpusGolden(t *testing.T) []string {
	t.Helper()
	f, err := os.Open("testdata/compact_corpus.golden")
	if err != nil {
		t.Fatalf("loadCorpusGolden: %v", err)
	}
	defer f.Close()

	var lines []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		lines = append(lines, line)
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("loadCorpusGolden scan: %v", err)
	}
	return lines
}

// TestCalibrateCorpus_CurrentConstantIsValid documents that BytesPerToken=3
// is within the plausible range for gg's mixed compact output. If this test
// fails after a corpus update, recalibrate the constant.
// The corpus is loaded from testdata/compact_corpus.golden — update that file
// when gg's output format changes significantly.
func TestCalibrateCorpus_CurrentConstantIsValid(t *testing.T) {
	samples := loadCorpusGolden(t)
	if len(samples) == 0 {
		t.Fatal("corpus golden file is empty — add representative compact lines")
	}
	r := CalibrateCorpus(samples)
	// BytesPerToken=3 should be within ±1 of the measured rounded ratio.
	diff := r.Rounded - BytesPerToken
	if diff < -1 || diff > 1 {
		t.Errorf("corpus rounded ratio = %d, BytesPerToken = %d: diff %d exceeds ±1 — consider recalibrating",
			r.Rounded, BytesPerToken, diff)
	}
}

// TestEmitCalibrationFactor tokenises the golden corpus, computes the
// bytes-per-actual-tokens ratio, and emits the measured factor via t.Logf
// so it is visible under `go test -v`. Run this test after updating
// testdata/compact_corpus.golden to see whether BytesPerToken needs updating.
func TestEmitCalibrationFactor(t *testing.T) {
	samples := loadCorpusGolden(t)
	if len(samples) == 0 {
		t.Fatal("corpus golden file is empty")
	}
	r := CalibrateCorpus(samples)
	t.Logf("corpus: %d lines, %d bytes, %d tokens",
		len(samples), r.TotalBytes, r.TotalTokens)
	t.Logf("measured ratio: %.3f bytes/token → rounded factor = %d", r.Ratio, r.Rounded)
	t.Logf("current BytesPerToken constant = %d (diff from measured: %+d)",
		BytesPerToken, BytesPerToken-r.Rounded)
}
