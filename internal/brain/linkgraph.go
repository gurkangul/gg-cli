package brain

import (
	"sort"
	"strings"
)

// linkgraph.go — TASK-518 traversable index over the derived link graph.
//
// links.go answers one hop ("what points at this"). Walking further — "what is
// two hops away from this bug" — needs the whole graph resolved at once, because
// a reference is only an edge if its target actually exists in the ledger.
//
// The graph is built from the folded JSONL in a single pass and kept in memory
// for the life of one command. It is deliberately NOT persisted: the derivation
// is cheap, and a cached copy is one more thing that can silently disagree with
// the ledger.
//
// Traversal is UNDIRECTED. For "what is related to this" the direction of the
// original reference is not what the caller cares about — a task that depends on
// this one and a decision this one references are equally part of the
// neighbourhood. Direction is still reported per edge so the caller can see it.

// LinkNode is one brain entry as a node in the derived link graph.
type LinkNode struct {
	Entry Entry
	Kind  string
	ID    string // TASK-/BUG- id when it has one, else the uuid
	Key   string // normalized lookup key
}

// LinkGraph is the resolved link graph for one project.
type LinkGraph struct {
	nodes    map[string]LinkNode // key -> node
	aliases  map[string]string   // lowercased title/text -> key
	out      map[string][]edge   // key -> resolved outgoing edges
	in       map[string][]edge   // key -> resolved incoming edges
	dangling map[string][]Ref    // key -> refs that resolved to nothing
}

type edge struct {
	To  string
	Via string
}

// Related is one node reached by a walk, with how far and how it was reached.
type Related struct {
	Node      LinkNode
	Hops      int
	Via       string
	From      string
	Direction string // "out" when the edge points away from From, "in" when toward it
}

// LoadLinkGraph reads every brain kind and resolves the link graph.
// Unreadable kinds are skipped rather than failing the whole traversal.
func LoadLinkGraph(ggDir string) (*LinkGraph, error) {
	g := &LinkGraph{
		nodes:    map[string]LinkNode{},
		aliases:  map[string]string{},
		out:      map[string][]edge{},
		in:       map[string][]edge{},
		dangling: map[string][]Ref{},
	}

	type pending struct {
		key  string
		refs []Ref
	}
	var all []pending

	for _, kind := range LinkKinds {
		entries, err := ReadLatest(ggDir, kind)
		if err != nil {
			continue
		}
		for _, e := range entries {
			id := SelfIDForKind(e, kind)
			key := NormalizeRef(id)
			if key == "" {
				continue
			}
			g.nodes[key] = LinkNode{Entry: e, Kind: kind, ID: id, Key: key}
			// The uuid is always a valid alias, even when the node is keyed by
			// its TASK-/BUG- id, so a uuid reference still resolves.
			if u := NormalizeRef(e.UUID); u != "" && u != key {
				g.aliases[u] = key
			}
			// Titles/texts let a [[By Title]] wikilink resolve. First writer wins:
			// a duplicate title must not silently retarget an existing alias.
			for _, field := range []string{"title", "text", "approach"} {
				if v := payloadString(e.Payload[field]); v != "" {
					alias := strings.ToLower(strings.TrimSpace(v))
					if _, exists := g.aliases[alias]; !exists {
						g.aliases[alias] = key
					}
				}
			}
			all = append(all, pending{key: key, refs: OutgoingRefsForKind(e, kind)})
		}
	}

	for _, p := range all {
		for _, ref := range p.refs {
			target, ok := g.resolve(ref.Target)
			if !ok || target == p.key {
				g.dangling[p.key] = append(g.dangling[p.key], ref)
				continue
			}
			g.out[p.key] = append(g.out[p.key], edge{To: target, Via: ref.Via})
			g.in[target] = append(g.in[target], edge{To: p.key, Via: ref.Via})
		}
	}
	return g, nil
}

// resolve maps a normalized reference to a node key, via direct key or alias.
func (g *LinkGraph) resolve(ref string) (string, bool) {
	if ref == "" {
		return "", false
	}
	if _, ok := g.nodes[ref]; ok {
		return ref, true
	}
	if key, ok := g.aliases[ref]; ok {
		return key, true
	}
	return "", false
}

// Lookup returns the node a reference points at.
func (g *LinkGraph) Lookup(ref string) (LinkNode, bool) {
	key, ok := g.resolve(NormalizeRef(ref))
	if !ok {
		return LinkNode{}, false
	}
	return g.nodes[key], true
}

// Dangling returns the references of a node that resolved to nothing.
func (g *LinkGraph) Dangling(ref string) []Ref {
	key, ok := g.resolve(NormalizeRef(ref))
	if !ok {
		return nil
	}
	return g.dangling[key]
}

// GraphEdge is one resolved edge, for callers that need the whole graph rather
// than a walk from one anchor (e.g. visualization export).
type GraphEdge struct {
	Src string
	Dst string
	Via string
}

// Nodes returns every node, ordered by key so exports are byte-stable across
// runs (map iteration order is not).
func (g *LinkGraph) Nodes() []LinkNode {
	out := make([]LinkNode, 0, len(g.nodes))
	for _, n := range g.nodes {
		out = append(out, n)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// Edges returns every resolved edge, deduped and in stable order.
func (g *LinkGraph) Edges() []GraphEdge {
	seen := map[string]bool{}
	var out []GraphEdge
	for src, edges := range g.out {
		for _, e := range edges {
			key := src + "\x00" + e.To + "\x00" + e.Via
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, GraphEdge{Src: src, Dst: e.To, Via: e.Via})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Src != out[j].Src {
			return out[i].Src < out[j].Src
		}
		if out[i].Dst != out[j].Dst {
			return out[i].Dst < out[j].Dst
		}
		return out[i].Via < out[j].Via
	})
	return out
}

// Related walks outward from ref up to maxHops and returns every node reached,
// nearest first. The anchor itself is excluded. Each node is reported once, at
// the shortest distance it was found (BFS order guarantees this).
func (g *LinkGraph) Related(ref string, maxHops int) []Related {
	start, ok := g.resolve(NormalizeRef(ref))
	if !ok || maxHops <= 0 {
		return nil
	}

	seen := map[string]bool{start: true}
	var out []Related
	frontier := []string{start}

	for hop := 1; hop <= maxHops && len(frontier) > 0; hop++ {
		var next []string
		for _, cur := range frontier {
			for _, e := range g.out[cur] {
				if seen[e.To] {
					continue
				}
				seen[e.To] = true
				out = append(out, Related{Node: g.nodes[e.To], Hops: hop, Via: e.Via, From: cur, Direction: "out"})
				next = append(next, e.To)
			}
			for _, e := range g.in[cur] {
				if seen[e.To] {
					continue
				}
				seen[e.To] = true
				out = append(out, Related{Node: g.nodes[e.To], Hops: hop, Via: e.Via, From: cur, Direction: "in"})
				next = append(next, e.To)
			}
		}
		frontier = next
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Hops != out[j].Hops {
			return out[i].Hops < out[j].Hops
		}
		return out[i].Node.ID < out[j].Node.ID
	})
	return out
}

// UnlinkedMentions returns entries whose prose names the target's title but
// which carry NO resolved reference to it — the "you wrote about this without
// linking it" signal.
//
// Scope is deliberately narrow: only the target's exact title/text is matched,
// never arbitrary substrings of the query. A noisy nudge is worse than none,
// because agents learn to ignore it.
func (g *LinkGraph) UnlinkedMentions(ref string) []LinkNode {
	anchor, ok := g.Lookup(ref)
	if !ok {
		return nil
	}
	title := ""
	for _, field := range []string{"title", "text", "approach"} {
		if v := payloadString(anchor.Entry.Payload[field]); v != "" {
			title = strings.TrimSpace(v)
			break
		}
	}
	// Very short titles match everything; refuse rather than flood the caller.
	if len(title) < 12 {
		return nil
	}
	needle := strings.ToLower(title)

	linked := map[string]bool{}
	for _, e := range g.in[anchor.Key] {
		linked[e.To] = true
	}
	for _, e := range g.out[anchor.Key] {
		linked[e.To] = true
	}

	var out []LinkNode
	for key, node := range g.nodes {
		if key == anchor.Key || linked[key] {
			continue
		}
		for _, field := range linkTextKeys {
			matched := false
			for _, text := range payloadStrings(node.Entry.Payload[field]) {
				if strings.Contains(strings.ToLower(text), needle) {
					out = append(out, node)
					matched = true
					break
				}
			}
			if matched {
				break
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
