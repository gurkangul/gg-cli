package brain

import (
	"regexp"
	"strings"
)

// links.go — TASK-517 derived link graph over the JSONL fold.
//
// gg has always carried relationships between brain entries — a decision names
// the task it implements (task_id), a task declares depends_on/blocks, a bug
// points at the task that fixes it — but they were only ever traversable as
// opt-in edges written into the DERIVED Memgraph store, and were never
// answerable in reverse. There was no way to ask "what links TO this decision".
//
// This computes the link graph as a pure read-time derivation over the folded
// JSONL, which is the canonical source of truth. That choice is deliberate:
//   - no schema change, so nothing to migrate and nothing that can rot,
//   - it keeps working with BOTH the embedder and Memgraph down, and
//   - it can never disagree with the ledger, because it IS the ledger.
//
// Two reference forms are recognised. Structured refs come from the payload
// fields gg already writes (task_id / depends_on / blocks). Prose refs are the
// Obsidian-style [[wiki link]] plus bare TASK-NNN / BUG-NNN mentions written in
// free text — so simply naming a task in a decision's reason now creates a
// traversable, reversible edge without any extra flag.
//
// Resolution is EXACT ONLY (id, or case-insensitive title/text equality). A
// [[ref]] that matches nothing is reported as unresolved rather than guessed at:
// a wrong edge in a memory graph is worse than a missing one.

var (
	// wikilinkPattern matches an Obsidian-style [[reference]].
	wikilinkPattern = regexp.MustCompile(`\[\[([^\[\]]{1,200})\]\]`)
	// idRefPattern matches a bare TASK-042 / BUG-084 mention in prose. The 3+
	// digit floor mirrors cmd.exactSearchID so both agree on what an ID looks like.
	idRefPattern = regexp.MustCompile(`\b(?:TASK|BUG)-\d{3,}\b`)
)

// LinkKinds are the brain kinds scanned when building the link graph.
var LinkKinds = []string{"decisions", "rejections", "tasks", "bugs", "notes", "messages"}

// linkTextKeys are payload fields whose free text is scanned for prose refs.
// The union across kinds is used; absent keys are simply skipped.
var linkTextKeys = []string{
	"text", "reason", "evidence", "title", "detail", "approach", "content",
	"root_cause", "fix_summary", "done_summary", "block_reason", "review_notes",
	"ready_for_live_plan", "rejected_alternatives",
}

// Ref is one normalized outgoing reference from a brain entry.
type Ref struct {
	Target string // normalized target: "TASK-042", "BUG-084", a uuid, or a lowercased title
	Via    string // how it was expressed: task_id, depends_on, blocks, wikilink, mention
	Raw    string // the reference exactly as written
}

// LinkHit is one entry that references a target, with the ways it did so.
type LinkHit struct {
	Entry Entry
	Kind  string
	Via   []string
}

// SelfID returns the canonical identifier another entry would use to reference
// e, inferring the kind from the entry itself.
func SelfID(e Entry) string { return SelfIDForKind(e, e.Kind) }

// SelfIDForKind is SelfID with the kind supplied by the caller. Legacy JSONL
// lines predate the top-level "kind" field, so an entry read from tasks.jsonl
// can arrive with Kind empty; without the caller's kind, a task's own task_id
// would be misread as an outgoing reference to a different task.
func SelfIDForKind(e Entry, kind string) string {
	if kind == "" {
		kind = e.Kind
	}
	switch kind {
	case "tasks":
		if id := payloadString(e.Payload["task_id"]); id != "" {
			return strings.ToUpper(id)
		}
	case "bugs":
		if id := payloadString(e.Payload["bug_id"]); id != "" {
			return strings.ToUpper(id)
		}
	}
	return e.UUID
}

// NormalizeRef puts a reference into the form used for comparison: TASK-/BUG-
// ids uppercased, everything else trimmed and lowercased so title matching is
// case-insensitive.
func NormalizeRef(ref string) string {
	r := strings.TrimSpace(ref)
	if r == "" {
		return ""
	}
	if idRefPattern.MatchString(strings.ToUpper(r)) && len(r) <= 20 {
		return strings.ToUpper(r)
	}
	if looksLikeUUID(r) {
		return strings.ToLower(r)
	}
	return strings.ToLower(r)
}

func looksLikeUUID(s string) bool {
	return len(s) == 36 && strings.Count(s, "-") == 4
}

// OutgoingRefs returns every reference e makes to another brain entry, from
// both structured payload fields and prose. Self-references are dropped.
func OutgoingRefs(e Entry) []Ref { return OutgoingRefsForKind(e, e.Kind) }

// OutgoingRefsForKind is OutgoingRefs with the kind supplied by the caller, so
// a task's own task_id is never mistaken for an outgoing edge on legacy lines
// that carry no top-level kind.
func OutgoingRefsForKind(e Entry, kind string) []Ref {
	if kind == "" {
		kind = e.Kind
	}
	self := strings.ToUpper(SelfIDForKind(e, kind))
	seen := map[string]bool{}
	var out []Ref

	add := func(raw, via string) {
		norm := NormalizeRef(raw)
		if norm == "" || strings.ToUpper(norm) == self || strings.EqualFold(norm, e.UUID) {
			return
		}
		key := norm + "|" + via
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, Ref{Target: norm, Via: via, Raw: strings.TrimSpace(raw)})
	}

	// Structured refs: the relationships gg already persists. A task's task_id
	// is its own identity, not an edge; every other kind uses it as a cross-ref
	// to the task the entry belongs to.
	if kind != "tasks" {
		if v := payloadString(e.Payload["task_id"]); v != "" {
			add(v, "task_id")
		}
	}
	for _, dep := range payloadStrings(e.Payload["depends_on"]) {
		add(dep, "depends_on")
	}
	for _, blk := range payloadStrings(e.Payload["blocks"]) {
		add(blk, "blocks")
	}

	// Prose refs: [[wiki links]] and bare TASK-/BUG- mentions.
	for _, key := range linkTextKeys {
		for _, text := range payloadStrings(e.Payload[key]) {
			for _, m := range wikilinkPattern.FindAllStringSubmatch(text, -1) {
				add(m[1], "wikilink")
			}
			for _, m := range idRefPattern.FindAllString(strings.ToUpper(text), -1) {
				add(m, "mention")
			}
		}
	}
	return out
}

// payloadString coerces a single payload value to a trimmed string.
func payloadString(v any) string {
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

// Backlinks returns every entry across all brain kinds that references target.
// target may be a TASK-/BUG- id, a uuid, or an exact title/text.
//
// It reads the FOLDED state per kind (ReadLatest), so superseded revisions of an
// entry never produce phantom backlinks. Unreadable kinds are skipped rather
// than failing the whole traversal.
func Backlinks(ggDir, target string) ([]LinkHit, error) {
	want := NormalizeRef(target)
	if want == "" {
		return nil, nil
	}
	var hits []LinkHit
	for _, kind := range LinkKinds {
		entries, err := ReadLatest(ggDir, kind)
		if err != nil {
			continue
		}
		for _, e := range entries {
			var via []string
			for _, ref := range OutgoingRefsForKind(e, kind) {
				if ref.Target == want {
					via = append(via, ref.Via)
				}
			}
			if len(via) > 0 {
				hits = append(hits, LinkHit{Entry: e, Kind: kind, Via: via})
			}
		}
	}
	return hits, nil
}

// FindByRef locates the entry a reference points at, matching on id first and
// then on exact (case-insensitive) title/text. Returns ok=false when nothing
// matches — the caller reports an unresolved reference instead of guessing.
func FindByRef(ggDir, ref string) (Entry, string, bool) {
	want := NormalizeRef(ref)
	if want == "" {
		return Entry{}, "", false
	}
	for _, kind := range LinkKinds {
		entries, err := ReadLatest(ggDir, kind)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if strings.EqualFold(SelfIDForKind(e, kind), want) || strings.EqualFold(e.UUID, want) {
				return e, kind, true
			}
		}
	}
	// Second pass: exact title/text match.
	for _, kind := range LinkKinds {
		entries, err := ReadLatest(ggDir, kind)
		if err != nil {
			continue
		}
		for _, e := range entries {
			for _, key := range []string{"title", "text", "approach"} {
				if v := payloadString(e.Payload[key]); v != "" && strings.EqualFold(strings.TrimSpace(v), want) {
					return e, kind, true
				}
			}
		}
	}
	return Entry{}, "", false
}
