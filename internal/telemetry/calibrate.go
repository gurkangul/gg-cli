package telemetry

import (
	"unicode"
	"unicode/utf8"
)

// CountTokens returns an estimate of the number of tokens a string will use
// when processed by a BPE-style tokeniser (e.g. cl100k_base used by GPT-4 and
// Claude). The estimate is produced by a character-class segmentation heuristic
// that avoids any external dependency:
//
//   - Runs of ASCII letters and/or digits form one token.
//   - Each punctuation / symbol rune is one token (they almost never merge).
//   - Whitespace (spaces, tabs, newlines) is not counted as a token on its
//     own; it typically merges with the following word token in real BPE.
//   - Non-ASCII letters (e.g. Turkish "ğ", "ş") are treated as one token per
//     rune — conservative but safe for mixed-language CLI output.
//
// The result is intentionally approximate. Use it for relative comparisons
// (e.g. calibrating BytesPerToken) not for billing.
func CountTokens(s string) int {
	tokens := 0
	inAlnum := false // currently inside a letter/digit run

	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		i += size

		switch {
		case r == utf8.RuneError:
			// Invalid UTF-8 byte — count as one token, reset alnum run.
			inAlnum = false
			tokens++

		case r <= 0x7F && (isASCIILetter(r) || (r >= '0' && r <= '9')):
			// ASCII alphanumeric: extend or start a run.
			if !inAlnum {
				tokens++
				inAlnum = true
			}

		case unicode.IsSpace(r):
			// Whitespace: terminates an alnum run, not counted on its own.
			inAlnum = false

		case r <= 0x7F:
			// ASCII punctuation / symbol: one token each, terminates alnum run.
			inAlnum = false
			tokens++

		case unicode.IsLetter(r):
			// Non-ASCII letter (e.g. Turkish, emoji base): one token per rune.
			inAlnum = false
			tokens++

		default:
			// Other Unicode (emoji continuation, CJK, symbols): one token per rune.
			inAlnum = false
			tokens++
		}
	}
	return tokens
}

func isASCIILetter(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

// BytesPerTokenFor returns the bytes-per-token ratio for s, rounded to the
// nearest integer. Returns 0 if s is empty or CountTokens returns 0.
// Use this to calibrate BytesPerToken against representative gg output.
func BytesPerTokenFor(s string) int {
	if len(s) == 0 {
		return 0
	}
	tokens := CountTokens(s)
	if tokens == 0 {
		return 0
	}
	// Round to nearest integer.
	return (len(s) + tokens/2) / tokens
}

// CalibrateCorpus returns a CalibrateResult over a set of representative
// strings. It computes the median bytes-per-token across all non-empty entries,
// which is more robust to outliers than the mean.
func CalibrateCorpus(samples []string) CalibrateResult {
	var totalBytes, totalTokens int
	for _, s := range samples {
		if s == "" {
			continue
		}
		totalBytes += len(s)
		totalTokens += CountTokens(s)
	}
	if totalTokens == 0 {
		return CalibrateResult{}
	}
	ratio := float64(totalBytes) / float64(totalTokens)
	rounded := int((ratio * 2) + 0.5) // round to nearest 0.5, then *2 gives int×2
	rounded /= 2
	if rounded < 1 {
		rounded = 1
	}
	return CalibrateResult{
		TotalBytes:  totalBytes,
		TotalTokens: totalTokens,
		Ratio:       ratio,
		Rounded:     rounded,
	}
}

// CalibrateResult holds the output of CalibrateCorpus.
type CalibrateResult struct {
	TotalBytes  int
	TotalTokens int
	// Ratio is the exact bytes-per-token float (TotalBytes / TotalTokens).
	Ratio float64
	// Rounded is the ratio rounded to the nearest integer — the value to
	// use as BytesPerToken if the corpus is representative.
	Rounded int
}
