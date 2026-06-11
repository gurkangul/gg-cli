package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var brainFileName = regexp.MustCompile(`^[a-z][a-z-]*$`)

// handleFiles lists the JSONL source-of-truth files so the dashboard can show
// where the brain physically lives and how big each store is.
func (s *dashboardServer) handleFiles(w http.ResponseWriter, r *http.Request) {
	pc := s.resolveOr(w, r)
	if pc == nil {
		return
	}
	ggDir := pc.ggDir
	type fileInfo struct {
		Name    string `json:"name"`
		Records int    `json:"records"`
		Bytes   int64  `json:"bytes"`
	}
	var out []fileInfo
	add := func(path, name string) {
		fi, statErr := os.Stat(path)
		if statErr != nil {
			return
		}
		out = append(out, fileInfo{Name: name, Records: countFileLines(path), Bytes: fi.Size()})
	}
	brain := filepath.Join(ggDir, "brain")
	if entries, derr := os.ReadDir(brain); derr == nil {
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".jsonl") {
				add(filepath.Join(brain, e.Name()), strings.TrimSuffix(e.Name(), ".jsonl"))
			}
		}
	}
	add(filepath.Join(ggDir, "canon.jsonl"), "canon")
	sort.Slice(out, func(i, j int) bool { return out[i].Bytes > out[j].Bytes })
	writeJSONResp(w, out)
}

// handleFile returns the most recent parsed records of one JSONL store so the
// raw history is browsable. Name is whitelisted (no path traversal); tail capped.
func (s *dashboardServer) handleFile(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if !brainFileName.MatchString(name) {
		writeJSONResp(w, map[string]any{"error": "invalid file name"})
		return
	}
	tail := 30
	if n, _ := strconv.Atoi(r.URL.Query().Get("tail")); n > 0 && n <= 100 {
		tail = n
	}
	pc := s.resolveOr(w, r)
	if pc == nil {
		return
	}
	ggDir := pc.ggDir
	path := filepath.Join(ggDir, "brain", name+".jsonl")
	if name == "canon" {
		path = filepath.Join(ggDir, "canon.jsonl")
	}
	var recs []any
	for _, ln := range tailFileLines(path, tail) {
		var m any
		if json.Unmarshal([]byte(ln), &m) == nil {
			recs = append(recs, m)
		}
	}
	writeJSONResp(w, map[string]any{"name": name, "records": recs})
}

func countFileLines(path string) int {
	data, err := os.ReadFile(path) //nolint:gosec // path is a .gg store file resolved from config, not user input
	if err != nil {
		return 0
	}
	return strings.Count(string(data), "\n")
}

func tailFileLines(path string, n int) []string {
	data, err := os.ReadFile(path) //nolint:gosec // whitelisted store name under the resolved .gg dir
	if err != nil {
		return nil
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}

type graphNode struct {
	ID         string         `json:"id"`
	Label      string         `json:"label"`
	Properties map[string]any `json:"properties"`
}
type graphEdge struct {
	Src  string `json:"src"`
	Dst  string `json:"dst"`
	Type string `json:"type"`
}

// handleGraph builds the brain relationship graph from the store (not Memgraph,
// whose brain edges aren't reliably synced per project): task↔task dependencies
// and the decisions/bugs linked to tasks. Only connected records are returned,
// so the view stays legible and excludes the huge code graph entirely.
func (s *dashboardServer) handleGraph(w http.ResponseWriter, r *http.Request) {
	pc := s.resolveOr(w, r)
	if pc == nil {
		return
	}
	ctx := r.Context()
	tasks, _ := pc.store.ListTasks(ctx, "")
	decs, _ := pc.store.ListDecisions(ctx, 0, false)
	bugs, _ := pc.store.ListBugs(ctx, "")

	taskExists := map[string]bool{}
	for _, t := range tasks {
		taskExists[t.ID] = true
	}

	var edges []graphEdge
	used := map[string]bool{}
	add := func(src, dst, typ string) {
		edges = append(edges, graphEdge{Src: src, Dst: dst, Type: typ})
		used[src] = true
		used[dst] = true
	}
	for _, t := range tasks {
		for _, dep := range t.DependsOn {
			if taskExists[dep] {
				add(t.ID, dep, "DEPENDS_ON")
			}
		}
		for _, blk := range t.Blocks {
			if taskExists[blk] {
				add(t.ID, blk, "BLOCKS")
			}
		}
	}
	for _, d := range decs {
		if d.TaskID != "" && taskExists[d.TaskID] {
			add(d.ID, d.TaskID, "DECIDES")
		}
	}
	for _, b := range bugs {
		if b.TaskID != "" && taskExists[b.TaskID] {
			add(b.ID, b.TaskID, "AFFECTS")
		}
	}

	var nodes []graphNode
	seen := map[string]bool{}
	addNode := func(id, label, title string) {
		if !used[id] || seen[id] {
			return
		}
		seen[id] = true
		nodes = append(nodes, graphNode{ID: id, Label: label, Properties: map[string]any{"title": title}})
	}
	for _, t := range tasks {
		addNode(t.ID, "Task", t.Title)
	}
	for _, d := range decs {
		addNode(d.ID, "Decision", d.Text)
	}
	for _, b := range bugs {
		addNode(b.ID, "Bug", b.Title)
	}
	writeJSONResp(w, map[string]any{"nodes": nodes, "edges": edges})
}

// handleStream is a Server-Sent Events endpoint (TASK-476) that emits a "change"
// event whenever the JSONL brain changes, so the dashboard auto-refreshes as
// agents write. It polls the brain dir's mtime while a browser is connected and
// stops when the request context is cancelled — a foreground stream, not a
// daemon (consistent with gg serve's no-daemon model).
func (s *dashboardServer) handleStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	id := strings.TrimSpace(r.URL.Query().Get("project"))
	if id == "" {
		id = s.defaultID
	}
	pc, err := s.clientFor(id)
	if err != nil {
		return
	}
	brain := filepath.Join(pc.ggDir, "brain")
	last := brainMtime(brain)
	fmt.Fprint(w, "event: ready\ndata: ok\n\n")
	flusher.Flush()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			if m := brainMtime(brain); m != last {
				last = m
				fmt.Fprintf(w, "event: change\ndata: %d\n\n", m)
				flusher.Flush()
			}
		}
	}
}

// brainMtime returns the newest modification time (unix nano) across the brain
// JSONL files — a cheap change signal for the SSE stream.
func brainMtime(dir string) int64 {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	var newest int64
	for _, e := range entries {
		if info, ierr := e.Info(); ierr == nil {
			if t := info.ModTime().UnixNano(); t > newest {
				newest = t
			}
		}
	}
	return newest
}
