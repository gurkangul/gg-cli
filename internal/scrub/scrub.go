package scrub

import "regexp"

// secretPattern pairs a compiled regex with a short label used in warnings.
type secretPattern struct {
	label string
	re    *regexp.Regexp
}

var patterns = []secretPattern{
	{"Anthropic/OpenAI key", regexp.MustCompile(`sk-[A-Za-z0-9][A-Za-z0-9-]{19,}`)},
	{"GitHub personal token", regexp.MustCompile(`ghp_[A-Za-z0-9]{36}`)},
	{"GitHub OAuth token", regexp.MustCompile(`gho_[A-Za-z0-9]+`)},
	{"GitHub server-to-server token", regexp.MustCompile(`ghs_[A-Za-z0-9]+`)},
	{"GitHub user-to-server token", regexp.MustCompile(`ghu_[A-Za-z0-9]+`)},
	{"AWS access key", regexp.MustCompile(`AKIA[0-9A-Z]{16}`)},
	{"PEM private key", regexp.MustCompile(`-----BEGIN [A-Z ]+ PRIVATE KEY-----`)},
	{"Slack token", regexp.MustCompile(`xox[baprs]-[0-9A-Za-z-]+`)},
}

const Redacted = "[REDACTED]"

// Match reports whether the given pattern label matches a secret in s.
// Used for diagnostic output — callers can iterate Patterns() to find which ones fired.
func Patterns() []string {
	out := make([]string, len(patterns))
	for i, p := range patterns {
		out[i] = p.label
	}
	return out
}

// String replaces all secret patterns in s with [REDACTED].
// Returns the scrubbed string and the number of replacements made.
func String(s string) (string, int) {
	count := 0
	for _, p := range patterns {
		locs := p.re.FindAllStringIndex(s, -1)
		if len(locs) > 0 {
			s = p.re.ReplaceAllString(s, Redacted)
			count += len(locs)
		}
	}
	return s, count
}

// Any recursively scrubs string values inside maps, slices, and plain strings.
// Returns the (possibly mutated) value and the total replacement count.
// Maps and slices are mutated in place for efficiency.
func Any(v any) (any, int) {
	switch val := v.(type) {
	case string:
		s, n := String(val)
		return s, n
	case map[string]any:
		total := 0
		for k, mv := range val {
			scrubbed, n := Any(mv)
			if n > 0 {
				val[k] = scrubbed
			}
			total += n
		}
		return val, total
	case []any:
		total := 0
		for i, item := range val {
			scrubbed, n := Any(item)
			if n > 0 {
				val[i] = scrubbed
			}
			total += n
		}
		return val, total
	default:
		return v, 0
	}
}
