package templates

import (
	"crypto/sha256"
	"fmt"
	"regexp"
	"strings"
)

const (
	HookTemplateSHA256Header = "gg-template-sha256"
	HookTemplateNameHeader   = "gg-template-name"
)

var hookTemplateSHARe = regexp.MustCompile(`^[0-9a-f]{64}$`)

// HookTemplateHash returns the SHA256 for the deployable hook body with gg's
// marker metadata removed. The marker cannot hash itself, so the managed body
// excluding those two metadata lines is the stable comparison payload.
func HookTemplateHash(body string) string {
	clean := StripHookTemplateMarker(body)
	sum := sha256.Sum256([]byte(clean))
	return fmt.Sprintf("%x", sum)
}

// WithHookTemplateMarker inserts gg's template marker lines after the shebang.
func WithHookTemplateMarker(name, body string) string {
	clean := StripHookTemplateMarker(body)
	sha := HookTemplateHash(clean)
	marker := fmt.Sprintf("# %s: %s\n# %s: %s\n", HookTemplateSHA256Header, sha, HookTemplateNameHeader, name)
	if strings.HasPrefix(clean, "#!") {
		if idx := strings.Index(clean, "\n"); idx >= 0 {
			return clean[:idx+1] + marker + clean[idx+1:]
		}
		return clean + "\n" + marker
	}
	return marker + clean
}

// ParseHookTemplateMarker returns the deployed marker values when both marker
// lines are present and the SHA is well-formed.
func ParseHookTemplateMarker(body string) (name string, sha string, ok bool) {
	lines := strings.Split(body, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "# "+HookTemplateSHA256Header+":") {
			sha = strings.TrimSpace(strings.TrimPrefix(trimmed, "# "+HookTemplateSHA256Header+":"))
			continue
		}
		if strings.HasPrefix(trimmed, "# "+HookTemplateNameHeader+":") {
			name = strings.TrimSpace(strings.TrimPrefix(trimmed, "# "+HookTemplateNameHeader+":"))
		}
	}
	return name, sha, name != "" && hookTemplateSHARe.MatchString(sha)
}

// StripHookTemplateMarker removes gg's marker lines while preserving every
// other line byte-for-byte, including the trailing newline shape.
func StripHookTemplateMarker(body string) string {
	lines := strings.SplitAfter(body, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(strings.TrimSuffix(line, "\n"))
		if strings.HasPrefix(trimmed, "# "+HookTemplateSHA256Header+":") ||
			strings.HasPrefix(trimmed, "# "+HookTemplateNameHeader+":") {
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "")
}
