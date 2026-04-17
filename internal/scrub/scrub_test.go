package scrub_test

import (
	"strings"
	"testing"

	"github.com/gurkangul/gg-cli/internal/scrub"
)

func TestString_Positive(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"anthropic/openai key", "sk-" + strings.Repeat("a", 20)},
		{"github personal token", "ghp_" + strings.Repeat("A", 36)},
		{"github oauth token", "gho_" + strings.Repeat("B", 10)},
		{"github server-to-server", "ghs_" + strings.Repeat("C", 10)},
		{"github user-to-server", "ghu_" + strings.Repeat("D", 10)},
		{"aws access key", "AKIA" + strings.Repeat("A", 16)},
		{"pem private key header", "-----BEGIN RSA PRIVATE KEY-----"},
		{"slack bot token", "xoxb-123456-abcDEF"},
		{"slack app token", "xoxa-abc123"},
		{"slack refresh token", "xoxr-abc123"},
		{"slack legacy token", "xoxs-abc123"},
		{"slack user token", "xoxp-abc123"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, n := scrub.String(tc.input)
			if n == 0 {
				t.Errorf("expected match for %q, got 0 replacements (output: %q)", tc.input, got)
			}
			if strings.Contains(got, tc.input) {
				t.Errorf("original secret still present in output: %q", got)
			}
			if !strings.Contains(got, scrub.Redacted) {
				t.Errorf("output does not contain [REDACTED]: %q", got)
			}
		})
	}
}

func TestString_Negative(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"plain text", "hello world"},
		{"sk too short", "sk-abc"},
		{"ghp too short", "ghp_" + strings.Repeat("A", 35)},
		{"aws wrong prefix", "BKIA" + strings.Repeat("A", 16)},
		{"pem public key", "-----BEGIN PUBLIC KEY-----"},
		{"xox unknown char", "xoxz-abc123"},
		{"empty string", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, n := scrub.String(tc.input)
			if n != 0 {
				t.Errorf("expected 0 replacements for %q, got %d (output: %q)", tc.input, n, got)
			}
		})
	}
}

func TestString_EmbeddedInText(t *testing.T) {
	key := "sk-" + strings.Repeat("z", 20)
	input := "my API key is " + key + " and that's it"
	got, n := scrub.String(input)
	if n != 1 {
		t.Errorf("expected 1 replacement, got %d", n)
	}
	if strings.Contains(got, key) {
		t.Errorf("key still present in output: %q", got)
	}
	if !strings.HasPrefix(got, "my API key is ") {
		t.Errorf("surrounding text should be preserved: %q", got)
	}
}

func TestAny_Map(t *testing.T) {
	input := map[string]any{
		"text":   "normal value",
		"secret": "sk-" + strings.Repeat("x", 20),
		"nested": map[string]any{
			"token": "ghp_" + strings.Repeat("Y", 36),
		},
	}
	_, n := scrub.Any(input)
	if n != 2 {
		t.Errorf("expected 2 replacements in nested map, got %d", n)
	}
	if input["text"] != "normal value" {
		t.Errorf("non-secret field should be unchanged")
	}
	if input["secret"] != scrub.Redacted {
		t.Errorf("top-level secret not redacted: %v", input["secret"])
	}
	nested := input["nested"].(map[string]any)
	if nested["token"] != scrub.Redacted {
		t.Errorf("nested secret not redacted: %v", nested["token"])
	}
}

func TestAny_Slice(t *testing.T) {
	key := "AKIA" + strings.Repeat("B", 16)
	input := []any{"safe", key, 42}
	_, n := scrub.Any(input)
	if n != 1 {
		t.Errorf("expected 1 replacement in slice, got %d", n)
	}
	if input[0] != "safe" {
		t.Errorf("safe value changed: %v", input[0])
	}
	if input[1] != scrub.Redacted {
		t.Errorf("secret not redacted: %v", input[1])
	}
}

func TestAny_NonString(t *testing.T) {
	// Numeric and bool values should pass through unchanged.
	v, n := scrub.Any(42)
	if n != 0 || v != 42 {
		t.Errorf("int should be unchanged: n=%d v=%v", n, v)
	}
}

func TestString_MultipleMatchesSameString(t *testing.T) {
	key1 := "sk-" + strings.Repeat("a", 20)
	key2 := "sk-" + strings.Repeat("b", 20)
	input := key1 + " and " + key2
	got, n := scrub.String(input)
	if n != 2 {
		t.Errorf("expected 2 replacements, got %d", n)
	}
	if strings.Contains(got, key1) || strings.Contains(got, key2) {
		t.Errorf("secrets still present: %q", got)
	}
}
