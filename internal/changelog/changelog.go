// Package changelog parses changelog content and extracts entries between two
// version strings, used by session-start to surface a version-delta notice
// when the CLI has been upgraded.
//
// The actual CHANGELOG.md content is injected via SetContent — called once at
// startup by cmd/session_start.go which embeds the file at the cmd/ level.
package changelog

import (
	"strings"
)

var content string

// SetContent injects the raw CHANGELOG.md text. Call once at program startup
// before any Since/Versions calls.
func SetContent(s string) {
	content = s
}

// Since returns the changelog sections added between prevVersion and
// currVersion (exclusive of prevVersion, inclusive of currVersion and any
// intermediate versions). Versions follow the "## [X.Y.Z]" heading format.
//
// Special cases:
//   - prevVersion == "" → returns all sections through currVersion.
//   - currVersion == "dev" or not found → returns the [Unreleased] section.
//   - prevVersion == currVersion → returns "".
//
// The returned string is trimmed of leading/trailing blank lines.
func Since(prevVersion, currVersion string) string {
	return sinceFrom(content, prevVersion, currVersion)
}

// Versions returns all version strings present in the changelog, newest first.
// The special "Unreleased" section is represented by an empty string "".
func Versions() []string {
	return versionsFrom(content)
}

func sinceFrom(raw, prevVersion, currVersion string) string {
	if prevVersion == currVersion {
		return ""
	}
	lines := strings.Split(raw, "\n")

	// normalise: strip leading "v" so "v0.2.0" == "0.2.0"
	prev := strings.TrimPrefix(prevVersion, "v")
	curr := strings.TrimPrefix(currVersion, "v")

	type section struct {
		line int
		ver  string // "" for "Unreleased"
	}
	var sections []section
	for i, l := range lines {
		if !strings.HasPrefix(l, "## [") {
			continue
		}
		rest := l[4:]
		end := strings.Index(rest, "]")
		if end < 0 {
			continue
		}
		ver := rest[:end]
		if strings.EqualFold(ver, "unreleased") {
			sections = append(sections, section{i, ""})
		} else {
			sections = append(sections, section{i, ver})
		}
	}
	if len(sections) == 0 {
		return ""
	}

	currIdx := -1
	if curr == "" || curr == "dev" {
		for i, s := range sections {
			if s.ver == "" {
				currIdx = i
				break
			}
		}
	} else {
		for i, s := range sections {
			if s.ver == curr {
				currIdx = i
				break
			}
		}
		if currIdx < 0 {
			for i, s := range sections {
				if s.ver == "" {
					currIdx = i
					break
				}
			}
		}
	}
	if currIdx < 0 {
		return ""
	}

	prevIdx := len(sections)
	if prev != "" && prev != "dev" {
		for i, s := range sections {
			if s.ver == prev {
				prevIdx = i
				break
			}
		}
	}

	if currIdx >= prevIdx {
		return ""
	}

	startLine := sections[currIdx].line
	var endLine int
	if prevIdx < len(sections) {
		endLine = sections[prevIdx].line
	} else {
		endLine = len(lines)
	}

	excerpt := strings.Join(lines[startLine:endLine], "\n")
	return strings.TrimSpace(excerpt)
}

func versionsFrom(raw string) []string {
	var out []string
	for _, l := range strings.Split(raw, "\n") {
		if !strings.HasPrefix(l, "## [") {
			continue
		}
		rest := l[4:]
		end := strings.Index(rest, "]")
		if end < 0 {
			continue
		}
		ver := rest[:end]
		if strings.EqualFold(ver, "unreleased") {
			out = append(out, "")
		} else {
			out = append(out, ver)
		}
	}
	return out
}
