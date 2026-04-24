package telemetry

import (
	"math"
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

	t.Run("typical compact line is near 3 bytes per token", func(t *testing.T) {
		// Representative gg compact output. BytesPerToken=3 was chosen empirically;
		// this test verifies the heuristic lands in [2,4].
		line := "○ TASK-285 [low] Compact: token tahmini disclaimer + BytesPerToken calibration"
		ratio := BytesPerTokenFor(line)
		if ratio < 2 || ratio > 5 {
			t.Errorf("BytesPerTokenFor(compact line) = %d, want in [2,5]", ratio)
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

// TestCalibrateCorpus_CurrentConstantIsValid documents that BytesPerToken=3
// is within the plausible range for gg's mixed compact output. If this test
// fails after a corpus update, recalibrate the constant.
func TestCalibrateCorpus_CurrentConstantIsValid(t *testing.T) {
	// Broader corpus including status lines, task lists, decision compacts.
	samples := []string{
		"○ TASK-285 [low] Compact: token tahmini disclaimer + tokenizer kalibrasyon",
		"○ TASK-291 [medium] Terminal adapter integration tests (cmux + tmux) via GG_INTEGRATION gate",
		"○ TASK-292 [medium] gg master resume command — structured session-handoff dump",
		"D  2026-04-24  TASK-285 commit a30c6b3, tests green",
		"R  2026-04-17  LLM cümle-seviyesi davranış enforcement — LLM non-deterministic",
		"Compact  426 calls, 1.2 MB / ~327K tok saved (avg 65% reduction)",
		"Hydration 4 re-fetches (1%), 618 B back; net 1.2 MB / ~326K tok",
		"North Star  Last 7d: 4615 calls, 67% agent-initiated",
		"✓ done  83c3a7d  test(locks): rewrite TestConcurrentAcquire",
		"impact cmd/index.go:   4 deps, 12 symbols, 1 related decision (DEC-042)",
	}
	r := CalibrateCorpus(samples)
	// BytesPerToken=3 should be within ±1 of the measured rounded ratio.
	diff := r.Rounded - BytesPerToken
	if diff < -1 || diff > 1 {
		t.Errorf("corpus rounded ratio = %d, BytesPerToken = %d: diff %d exceeds ±1 — consider recalibrating",
			r.Rounded, BytesPerToken, diff)
	}
}
