package lsp

import (
	"encoding/json"
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf16"
)

// PathToURI converts an absolute filesystem path to a file:// URI. It percent-
// encodes path segments and prepends the leading slash Windows drive paths need.
func PathToURI(abs string) string {
	p := filepath.ToSlash(abs)
	if runtime.GOOS == "windows" && !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	segments := strings.Split(p, "/")
	for i, seg := range segments {
		segments[i] = url.PathEscape(seg)
	}
	return "file://" + strings.Join(segments, "/")
}

// URIToPath converts a file:// URI back to a filesystem path. A non-file URI or
// a parse error is returned unchanged so output never silently drops data.
func URIToPath(uri string) string {
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "file" {
		return uri
	}
	p := u.Path
	if runtime.GOOS == "windows" && len(p) >= 3 && p[0] == '/' && p[2] == ':' {
		p = p[1:]
	}
	return filepath.FromSlash(p)
}

// HumanToLSP converts a 1-based (line, col) editor position into a 0-based LSP
// Position with a UTF-16 character offset. fileText is the opened file's content;
// LSP counts characters in UTF-16 code units, so a column past a multibyte rune
// must be re-measured. Out-of-range inputs clamp to the nearest valid position.
func HumanToLSP(fileText string, line, col int) Position {
	lines := splitLines(fileText)
	lineIdx := clamp(line-1, 0, max(0, len(lines)-1))
	target := ""
	if lineIdx < len(lines) {
		target = lines[lineIdx]
	}
	// col is a 1-based offset in runes/visible chars; clamp to the line length+1.
	runeCol := clamp(col-1, 0, len([]rune(target)))
	runes := []rune(target)
	utf16Units := 0
	for i := 0; i < runeCol && i < len(runes); i++ {
		utf16Units += len(utf16.Encode([]rune{runes[i]}))
	}
	return Position{Line: lineIdx, Character: utf16Units}
}

// LSPToHuman converts a 0-based LSP Position (UTF-16 character offset) back into
// a 1-based (line, col) editor position counted in runes, for display.
func LSPToHuman(fileText string, pos Position) (line, col int) {
	lines := splitLines(fileText)
	lineIdx := clamp(pos.Line, 0, max(0, len(lines)-1))
	target := ""
	if lineIdx < len(lines) {
		target = lines[lineIdx]
	}
	runes := []rune(target)
	utf16Units := 0
	runeCol := 0
	for runeCol < len(runes) && utf16Units < pos.Character {
		utf16Units += len(utf16.Encode([]rune{runes[runeCol]}))
		runeCol++
	}
	return lineIdx + 1, runeCol + 1
}

// splitLines splits on \n and trims a trailing \r so CRLF files map columns
// correctly. An empty string yields a single empty line.
func splitLines(s string) []string {
	raw := strings.Split(s, "\n")
	for i, l := range raw {
		raw[i] = strings.TrimSuffix(l, "\r")
	}
	return raw
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// locationLink is the LSP 3.14+ alternate result for definition: the target
// range lives in targetUri/targetRange instead of uri/range.
type locationLink struct {
	TargetURI            string `json:"targetUri"`
	TargetRange          Range  `json:"targetRange"`
	TargetSelectionRange Range  `json:"targetSelectionRange"`
}

// decodeLocations normalizes a references/definition result, which may be: null,
// a single Location object, an array of Location, or an array of LocationLink.
func decodeLocations(raw json.RawMessage) ([]Location, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return nil, nil
	}
	// Array form (the common case for references and most definitions).
	if trimmed[0] == '[' {
		var links []json.RawMessage
		if err := json.Unmarshal(raw, &links); err != nil {
			return nil, err
		}
		out := make([]Location, 0, len(links))
		for _, item := range links {
			loc, ok := decodeOneLocation(item)
			if ok {
				out = append(out, loc)
			}
		}
		return out, nil
	}
	// Single-object form (some servers answer definition with one Location).
	if loc, ok := decodeOneLocation(raw); ok {
		return []Location{loc}, nil
	}
	return nil, nil
}

// decodeOneLocation decodes a single item as either a Location or a
// LocationLink, returning ok=false when it has neither a uri nor a targetUri.
func decodeOneLocation(item json.RawMessage) (Location, bool) {
	var loc Location
	if err := json.Unmarshal(item, &loc); err == nil && loc.URI != "" {
		return loc, true
	}
	var link locationLink
	if err := json.Unmarshal(item, &link); err == nil && link.TargetURI != "" {
		return Location{URI: link.TargetURI, Range: link.TargetRange}, true
	}
	return Location{}, false
}

// markupContent is the modern {kind, value} hover payload.
type markupContent struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

// hoverEnvelope is the textDocument/hover result wrapper. contents is decoded
// generically because it can be a string, {kind,value}, {language,value}, or an
// array of those.
type hoverEnvelope struct {
	Contents json.RawMessage `json:"contents"`
	Range    *Range          `json:"range"`
}

// decodeHover normalizes the various legal hover.contents shapes into plain text.
func decodeHover(raw json.RawMessage) (HoverResult, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return HoverResult{}, nil
	}
	var env hoverEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return HoverResult{}, err
	}
	return HoverResult{PlainText: flattenContents(env.Contents), Range: env.Range}, nil
}

// flattenContents folds a hover.contents value (string | MarkupContent |
// MarkedString | array thereof) into a single newline-joined plain-text string.
func flattenContents(raw json.RawMessage) string {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return ""
	}
	switch trimmed[0] {
	case '"':
		var s string
		if json.Unmarshal(raw, &s) == nil {
			return s
		}
	case '{':
		var mc markupContent
		if json.Unmarshal(raw, &mc) == nil && mc.Value != "" {
			return mc.Value
		}
		// MarkedString {language, value}
		var ms struct {
			Value string `json:"value"`
		}
		if json.Unmarshal(raw, &ms) == nil {
			return ms.Value
		}
	case '[':
		var items []json.RawMessage
		if json.Unmarshal(raw, &items) == nil {
			parts := make([]string, 0, len(items))
			for _, it := range items {
				if s := flattenContents(it); s != "" {
					parts = append(parts, s)
				}
			}
			return strings.Join(parts, "\n")
		}
	}
	return ""
}
