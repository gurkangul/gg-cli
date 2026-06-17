package store

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
)

// BrainRecord is a single exported stored point — payload only, no vector.
type BrainRecord struct {
	ID      string         `json:"id"`
	Payload map[string]any `json:"payload"`
}

// BrainKind maps vector-store collection suffixes to their canonical export file names.
var BrainKind = []string{
	collSuffixDecisions,
	collSuffixTasks,
	collSuffixMessages,
	collSuffixRejections,
	collSuffixDiscussions,
	collSuffixNotes,
	collSuffixBugs,
}

// ExportBrainCollection scrolls a single vector-store collection and returns its
// records sorted by ID — payload only, no vectors.
func (c *Client) ExportBrainCollection(ctx context.Context, kind string) ([]BrainRecord, error) {
	coll := c.projectID + "-" + kind
	pageSize := uint32(256)
	withVecs := NewWithVectorsEnable(false)
	withPay := NewWithPayloadEnable(true)

	var all []BrainRecord
	var offset *PointId
	for {
		page, next, err := c.scroller.ScrollAndOffset(ctx, &ScrollPoints{
			CollectionName: coll,
			Limit:          &pageSize,
			Offset:         offset,
			WithPayload:    withPay,
			WithVectors:    withVecs,
		})
		if err != nil {
			if isCollectionNotFoundError(err) {
				// Collection missing on fresh install — legitimate empty.
				return nil, nil
			}
			fmt.Fprintf(os.Stderr, "gg: brain export: transient scroll error for %q: %v\n", coll, err)
			return nil, fmt.Errorf("scroll %s: %w", coll, err)
		}
		for _, p := range page {
			id := p.GetId().GetUuid()
			if id == "" {
				continue
			}
			all = append(all, BrainRecord{
				ID:      id,
				Payload: extractPayload(p.GetPayload()),
			})
		}
		if next == nil {
			break
		}
		offset = next
	}

	// Deterministic order. UUIDs sort lexically; human-prefixed IDs
	// (TASK-/BUG-/DISC-N) sort by numeric suffix so TASK-1000 follows TASK-999
	// rather than preceding it (BUG-080 L1).
	sort.Slice(all, func(i, j int) bool { return brainIDLess(all[i].ID, all[j].ID) })
	return all, nil
}

// brainIDLess orders two record IDs. When both share the same non-numeric
// prefix and have a numeric suffix (e.g. "TASK-12" vs "TASK-3"), it compares the
// numbers; otherwise it falls back to a stable lexical compare (UUIDs).
func brainIDLess(a, b string) bool {
	pa, na, oka := splitPrefixNum(a)
	pb, nb, okb := splitPrefixNum(b)
	if oka && okb && pa == pb {
		if na != nb {
			return na < nb
		}
	}
	return a < b
}

// splitPrefixNum splits "TASK-123" into ("TASK-", 123, true). Returns ok=false
// when the string has no trailing digit run after a non-digit prefix.
func splitPrefixNum(s string) (prefix string, num int, ok bool) {
	i := len(s)
	for i > 0 && s[i-1] >= '0' && s[i-1] <= '9' {
		i--
	}
	if i == len(s) || i == 0 {
		return "", 0, false
	}
	n, err := strconv.Atoi(s[i:])
	if err != nil {
		return "", 0, false
	}
	return s[:i], n, true
}

// CanonicalJSON encodes v as compact JSON with:
//   - Map keys sorted alphabetically at every nesting level.
//   - Floats encoded with %.6f (six decimal places).
//   - No HTML-safe escaping (< > & are literal).
//   - No trailing newline (caller appends \n for JSONL).
func CanonicalJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	if err := writeCanonical(&buf, v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func writeCanonical(buf *bytes.Buffer, v any) error {
	if v == nil {
		buf.WriteString("null")
		return nil
	}
	switch x := v.(type) {
	case bool:
		if x {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}
	case int:
		buf.WriteString(strconv.Itoa(x))
	case int8:
		buf.WriteString(strconv.FormatInt(int64(x), 10))
	case int16:
		buf.WriteString(strconv.FormatInt(int64(x), 10))
	case int32:
		buf.WriteString(strconv.FormatInt(int64(x), 10))
	case int64:
		buf.WriteString(strconv.FormatInt(x, 10))
	case uint:
		buf.WriteString(strconv.FormatUint(uint64(x), 10))
	case uint8:
		buf.WriteString(strconv.FormatUint(uint64(x), 10))
	case uint16:
		buf.WriteString(strconv.FormatUint(uint64(x), 10))
	case uint32:
		buf.WriteString(strconv.FormatUint(uint64(x), 10))
	case uint64:
		buf.WriteString(strconv.FormatUint(x, 10))
	case float32:
		buf.WriteString(encodeFloat(float64(x)))
	case float64:
		buf.WriteString(encodeFloat(x))
	case string:
		writeJSONString(buf, x)
	case []any:
		buf.WriteByte('[')
		for i, item := range x {
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := writeCanonical(buf, item); err != nil {
				return err
			}
		}
		buf.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		buf.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				buf.WriteByte(',')
			}
			writeJSONString(buf, k)
			buf.WriteByte(':')
			if err := writeCanonical(buf, x[k]); err != nil {
				return err
			}
		}
		buf.WriteByte('}')
	default:
		return fmt.Errorf("canonical JSON: unsupported type %T", v)
	}
	return nil
}

// encodeFloat encodes a float64 as %.6f with no trailing zeros stripped.
// NaN and Inf are encoded as null (JSON has no representation for them).
func encodeFloat(f float64) string {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return "null"
	}
	return fmt.Sprintf("%.6f", f)
}

// writeJSONString writes a JSON-encoded string to buf without HTML escaping.
// Only the characters required by RFC 8259 §7 are escaped:
// " (U+0022), \ (U+005C), and control characters U+0000–U+001F.
func writeJSONString(buf *bytes.Buffer, s string) {
	buf.WriteByte('"')
	for i := 0; i < len(s); {
		b := s[i]
		if b < 0x80 {
			switch b {
			case '"':
				buf.WriteString(`\"`)
			case '\\':
				buf.WriteString(`\\`)
			case '\n':
				buf.WriteString(`\n`)
			case '\r':
				buf.WriteString(`\r`)
			case '\t':
				buf.WriteString(`\t`)
			default:
				if b < 0x20 {
					fmt.Fprintf(buf, `\u%04x`, b)
				} else {
					buf.WriteByte(b)
				}
			}
			i++
		} else {
			// Multi-byte UTF-8: copy bytes verbatim.
			// Find the full rune length.
			r := 2
			if b >= 0xF0 {
				r = 4
			} else if b >= 0xE0 {
				r = 3
			}
			if i+r > len(s) {
				r = len(s) - i
			}
			buf.WriteString(s[i : i+r])
			i += r
		}
	}
	buf.WriteByte('"')
}
