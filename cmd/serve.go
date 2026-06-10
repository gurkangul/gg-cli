package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/dashboard"
	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/embedding"
	"github.com/gurkangul/gg-cli/internal/store"
	"github.com/gurkangul/gg-cli/internal/telemetry"
)

var (
	servePort   int
	serveNoOpen bool
	serveWrite  bool
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Local dashboard — visualize every gg project's brain (decisions, work, live search)",
	Long: `gg serve starts a FOREGROUND, localhost-only web dashboard.

Unlike the other commands, it is path-independent: run it from anywhere and it
lists every gg project registered on this host (~/.gg/projects.json) and lets you
switch between them — each project's brain stays fully isolated (no merging). Run
inside a project and that project is selected by default.

It is NOT a daemon: it runs only until you press Ctrl-C, binds to 127.0.0.1
exclusively (no network exposure), and serves the same JSONL/Qdrant stores the
CLI reads.

  gg serve                # launcher for all projects at http://127.0.0.1:7777
  gg serve --port 8080    # pick another port
  gg serve --no-open      # do not open the browser automatically`,
	RunE: runServe,
}

func init() {
	serveCmd.Flags().IntVar(&servePort, "port", 7777, "localhost port to bind")
	serveCmd.Flags().BoolVar(&serveNoOpen, "no-open", false, "do not open the browser automatically")
	serveCmd.Flags().BoolVar(&serveWrite, "write", false, "enable write actions (record decision / create task) via POST — off by default")
	rootCmd.AddCommand(serveCmd)
}

func runServe(cmd *cobra.Command, _ []string) error {
	// Connection params (Qdrant/Ollama) are shared by every project, so use the
	// current project's config when inside one, else built-in defaults — that is
	// what lets gg serve run from any directory as a global launcher.
	base, berr := config.Load()
	if berr != nil {
		base = config.DefaultConfig()
	}
	srv := &dashboardServer{base: base, cache: map[string]*projClient{}}
	if base.ProjectID != "" {
		srv.defaultID = base.ProjectID
	} else if reg := srv.registry(); len(reg) > 0 {
		srv.defaultID = reg[0].ID
	}

	dist, err := fs.Sub(dashboard.FS, "dist")
	if err != nil {
		return fmt.Errorf("dashboard assets: %w", err)
	}
	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.FS(dist)))
	mux.HandleFunc("/api/projects", srv.handleProjects)
	mux.HandleFunc("/api/project-health", srv.handleProjectHealth)
	mux.HandleFunc("/api/overview", srv.handleOverview)
	mux.HandleFunc("/api/search", srv.handleSearch)
	mux.HandleFunc("/api/decisions", srv.handleDecisions)
	mux.HandleFunc("/api/tasks", srv.handleTasks)
	mux.HandleFunc("/api/bugs", srv.handleBugs)
	mux.HandleFunc("/api/telemetry", srv.handleTelemetry)
	mux.HandleFunc("/api/files", srv.handleFiles)
	mux.HandleFunc("/api/file", srv.handleFile)
	mux.HandleFunc("/api/graph", srv.handleGraph)
	mux.HandleFunc("/api/messages", srv.handleMessages)
	mux.HandleFunc("/api/stream", srv.handleStream)
	mux.HandleFunc("/api/write/decision", srv.handleWriteDecision)
	mux.HandleFunc("/api/write/task", srv.handleWriteTask)

	addr := fmt.Sprintf("127.0.0.1:%d", servePort)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("cannot bind %s: %w (try --port)", addr, err)
	}
	url := "http://" + addr
	httpSrv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}

	fmt.Printf("─── gg dashboard ───\n")
	fmt.Printf("  %s\n", url)
	fmt.Printf("  localhost-only, foreground (Ctrl-C to stop) — not a daemon\n")
	if n := len(srv.registry()); n > 0 {
		fmt.Printf("  %d project(s) registered — switch between them in the dashboard\n", n)
	} else {
		fmt.Println("  ⚠ no gg projects registered — run 'gg init' in a project first")
	}
	if !serveNoOpen {
		openBrowser(url)
	}

	errc := make(chan error, 1)
	go func() { errc <- httpSrv.Serve(ln) }()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	select {
	case <-sig:
		fmt.Println("\nshutting down…")
	case err := <-errc:
		srv.closeAll()
		return err
	}
	shutCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(shutCtx)
	srv.closeAll()
	return nil
}

// projClient is a per-project view of the brain: its own store + embedder,
// resolved lazily from the registry and cached. The shared local infra
// (Qdrant/Ollama) is reached with the base config; only the data dir and
// project_id differ per project, so one server can serve every project while
// keeping their brains fully isolated.
type projClient struct {
	store      *store.Client
	embedder   *embedding.Generator
	root       string
	ggDir      string
	runtimeDir string
	projectID  string
	qdrantDown bool
}

type dashboardServer struct {
	base      *config.Config // connection params (Qdrant/Ollama) shared by all projects
	defaultID string         // project selected when no ?project is given
	mu        sync.Mutex
	cache     map[string]*projClient
}

// registry returns the registered projects whose root still exists and is a gg
// project (filters out stale /tmp test projects), in stable order.
func (s *dashboardServer) registry() []config.ProjectEntry {
	reg, err := config.LoadRegistry()
	if err != nil {
		return nil
	}
	var out []config.ProjectEntry
	for _, p := range reg.Sorted() {
		if fi, statErr := os.Stat(filepath.Join(p.Root, config.DirName, "brain")); statErr == nil && fi.IsDir() {
			out = append(out, p)
		}
	}
	return out
}

// clientFor builds (or returns a cached) per-project client for a registered,
// live project id.
func (s *dashboardServer) clientFor(id string) (*projClient, error) {
	if id == "" {
		return nil, fmt.Errorf("no project selected")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if pc, ok := s.cache[id]; ok {
		return pc, nil
	}
	var entry *config.ProjectEntry
	for _, p := range s.registry() {
		if p.ID == id {
			e := p
			entry = &e
			break
		}
	}
	if entry == nil {
		return nil, fmt.Errorf("unknown project %q", id)
	}
	ggDir := filepath.Join(entry.Root, config.DirName)
	client, err := store.New(&s.base.Qdrant, ggDir, id)
	if err != nil {
		return nil, err
	}
	pc := &projClient{store: client, root: entry.Root, ggDir: ggDir, projectID: id}
	if sd, sderr := config.SharedDir(); sderr == nil {
		pc.runtimeDir = filepath.Join(sd, "projects", id)
	}
	hctx, hcancel := context.WithTimeout(context.Background(), healthCheckTimeout)
	defer hcancel()
	if hErr := client.HealthCheck(hctx); hErr != nil {
		pc.qdrantDown = true
	}
	dim := store.VectorSize
	if meta, readErr := embedding.ReadMeta(ggDir); readErr == nil && meta != nil {
		dim = meta.Dim
	}
	pc.embedder = embedding.New(&s.base.Embedding, dim)
	s.cache[id] = pc
	return pc, nil
}

// resolveOr resolves the project for a request (?project=<id>, else the default)
// and writes a JSON error + returns nil when it cannot.
func (s *dashboardServer) resolveOr(w http.ResponseWriter, r *http.Request) *projClient {
	id := strings.TrimSpace(r.URL.Query().Get("project"))
	if id == "" {
		id = s.defaultID
	}
	pc, err := s.clientFor(id)
	if err != nil {
		writeJSONResp(w, map[string]any{"error": err.Error()})
		return nil
	}
	return pc
}

// closeAll closes every cached project store on shutdown.
func (s *dashboardServer) closeAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, pc := range s.cache {
		if pc.store != nil {
			_ = pc.store.Close()
		}
	}
}

// handleProjectHealth returns one project's quick health for the launcher
// portfolio. Resolved per-request (?project=<id>) and loaded lazily by the UI so
// a host with many projects pays for counts only as cards render — not 11× up
// front. Brains stay isolated; this is navigation metadata, not a merged view.
func (s *dashboardServer) handleProjectHealth(w http.ResponseWriter, r *http.Request) {
	pc := s.resolveOr(w, r)
	if pc == nil {
		return
	}
	ctx := r.Context()
	tasks, _ := pc.store.ListTasks(ctx, "")
	bugs, _ := pc.store.ListBugs(ctx, "")
	decs, _ := pc.store.ListDecisions(ctx, 0, false)
	openTasks, openBugs, last := 0, 0, ""
	for _, t := range tasks {
		if t.Status != "done" {
			openTasks++
		}
		if t.CreatedAt > last {
			last = t.CreatedAt
		}
	}
	for _, b := range bugs {
		if b.Status == "open" || b.Status == "reopened" || b.Status == "fixing" {
			openBugs++
		}
	}
	for _, d := range decs {
		if d.CreatedAt > last {
			last = d.CreatedAt
		}
	}
	writeJSONResp(w, map[string]any{
		"id": pc.projectID, "openTasks": openTasks, "openBugs": openBugs,
		"decisions": len(decs), "lastActivity": last,
	})
}

// handleProjects lists the registered projects for the dashboard switcher.
func (s *dashboardServer) handleProjects(w http.ResponseWriter, _ *http.Request) {
	type projItem struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Root    string `json:"root"`
		Default bool   `json:"default"`
	}
	out := []projItem{}
	for _, p := range s.registry() {
		out = append(out, projItem{ID: p.ID, Name: p.Name, Root: p.Root, Default: p.ID == s.defaultID})
	}
	writeJSONResp(w, out)
}

func writeJSONResp(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (s *dashboardServer) handleOverview(w http.ResponseWriter, r *http.Request) {
	pc := s.resolveOr(w, r)
	if pc == nil {
		return
	}
	ctx := r.Context()
	decs, _ := pc.store.ListDecisions(ctx, 0, false)
	rejs, _ := pc.store.ListRejections(ctx, 0)
	bugs, _ := pc.store.ListBugs(ctx, "")
	tasks, _ := pc.store.ListTasks(ctx, "")
	fixed, _ := pc.store.ListBugs(ctx, "fixed")

	openBugs := 0
	for _, b := range bugs {
		if b.Status == "open" || b.Status == "reopened" || b.Status == "fixing" {
			openBugs++
		}
	}
	doneTasks, openTasks := 0, 0
	for _, t := range tasks {
		if t.Status == "done" {
			doneTasks++
		} else {
			openTasks++
		}
	}
	writeJSONResp(w, map[string]any{
		"project": pc.projectID,
		"counts": map[string]int{
			"decisions": len(decs), "rejections": len(rejs),
			"bugs": len(bugs), "bugsOpen": openBugs, "bugsFixed": len(fixed),
			"tasks": len(tasks), "tasksDone": doneTasks, "tasksOpen": openTasks,
		},
		"canon":           store.BuildAutoCanon(decs, rejs, fixed, tasks),
		"recentDecisions": firstN(store.FilterDecisionNoise(decs), 25),
		"writable":        serveWrite,
	})
}

func (s *dashboardServer) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeJSONResp(w, map[string]any{"error": "empty query"})
		return
	}
	pc := s.resolveOr(w, r)
	if pc == nil {
		return
	}
	if pc.qdrantDown || pc.embedder == nil {
		writeJSONResp(w, map[string]any{"error": "live search needs Qdrant + Ollama; stores are degraded"})
		return
	}
	ctx := r.Context()
	t0 := time.Now()
	vec, err := pc.embedder.Generate(ctx, q)
	embedMs := time.Since(t0).Milliseconds()
	if err != nil {
		writeJSONResp(w, map[string]any{"error": "embed failed: " + err.Error()})
		return
	}
	t1 := time.Now()
	decs, _ := pc.store.SearchDecisions(ctx, vec, 8, false)
	rejs, _ := pc.store.SearchRejections(ctx, vec, 5)
	bugs, _ := pc.store.SearchBugs(ctx, vec, 5, false)
	searchMs := time.Since(t1).Milliseconds()
	writeJSONResp(w, map[string]any{
		"query": q, "vectorDim": len(vec),
		"embedMs": embedMs, "searchMs": searchMs,
		"decisions": decs, "rejections": rejs, "bugs": bugs,
	})
}

func (s *dashboardServer) handleDecisions(w http.ResponseWriter, r *http.Request) {
	pc := s.resolveOr(w, r)
	if pc == nil {
		return
	}
	decs, _ := pc.store.ListDecisions(r.Context(), 0, false)
	writeJSONResp(w, store.FilterDecisionNoise(decs))
}

func (s *dashboardServer) handleTasks(w http.ResponseWriter, r *http.Request) {
	pc := s.resolveOr(w, r)
	if pc == nil {
		return
	}
	tasks, _ := pc.store.ListTasks(r.Context(), r.URL.Query().Get("status"))
	writeJSONResp(w, tasks)
}

func (s *dashboardServer) handleBugs(w http.ResponseWriter, r *http.Request) {
	pc := s.resolveOr(w, r)
	if pc == nil {
		return
	}
	bugs, _ := pc.store.ListBugs(r.Context(), r.URL.Query().Get("status"))
	writeJSONResp(w, bugs)
}

func (s *dashboardServer) handleTelemetry(w http.ResponseWriter, r *http.Request) {
	pc := s.resolveOr(w, r)
	if pc == nil {
		return
	}
	if pc.runtimeDir == "" {
		writeJSONResp(w, map[string]any{})
		return
	}
	wk, _ := telemetry.Summarize(pc.runtimeDir)
	sess, _ := telemetry.SummarizeSessions(pc.runtimeDir, time.Now().UTC().AddDate(0, 0, -7))
	writeJSONResp(w, map[string]any{"weekly": wk, "sessions": sess})
}

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

// handleMessages returns the recent agent-to-agent message stream (newest first,
// capped) so the dashboard can show how agents coordinate.
func (s *dashboardServer) handleMessages(w http.ResponseWriter, r *http.Request) {
	pc := s.resolveOr(w, r)
	if pc == nil {
		return
	}
	since := time.Now().AddDate(0, 0, -30)
	msgs, err := pc.store.ListMessagesSince(r.Context(), since)
	if err != nil {
		writeJSONResp(w, map[string]any{"error": err.Error()})
		return
	}
	sort.Slice(msgs, func(i, j int) bool { return msgs[i].CreatedAt > msgs[j].CreatedAt })
	if len(msgs) > 200 {
		msgs = msgs[:200]
	}
	writeJSONResp(w, msgs)
}

// requireWrite gates the write endpoints: POST only, opt-in via --write, and a
// same-origin (localhost) check to block cross-site writes against the local server.
func (s *dashboardServer) requireWrite(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return false
	}
	if !serveWrite {
		writeJSONResp(w, map[string]any{"error": "read-only — start with 'gg serve --write' to enable write actions"})
		return false
	}
	if o := r.Header.Get("Origin"); o != "" && !strings.HasPrefix(o, "http://127.0.0.1") && !strings.HasPrefix(o, "http://localhost") {
		http.Error(w, "cross-origin write rejected", http.StatusForbidden)
		return false
	}
	return true
}

// ggExec runs this same gg binary as a subprocess so write actions reuse the
// exact CLI logic (embedding, JSONL, Qdrant, graph, gates) — the server is gg.
// dir is the selected project's root so the write lands in the right brain.
func (s *dashboardServer) ggExec(ctx context.Context, dir string, args ...string) (string, error) {
	exe, err := os.Executable()
	if err != nil {
		exe = "gg"
	}
	c := exec.CommandContext(ctx, exe, args...) //nolint:gosec // fixed gg subcommands; values come from validated JSON
	c.Env = os.Environ()
	if dir != "" {
		c.Dir = dir
	}
	out, cerr := c.CombinedOutput()
	return strings.TrimSpace(string(out)), cerr
}

func (s *dashboardServer) handleWriteDecision(w http.ResponseWriter, r *http.Request) {
	if !s.requireWrite(w, r) {
		return
	}
	pc := s.resolveOr(w, r)
	if pc == nil {
		return
	}
	var body struct{ Text, Reason string }
	if json.NewDecoder(r.Body).Decode(&body) != nil || strings.TrimSpace(body.Text) == "" {
		writeJSONResp(w, map[string]any{"error": "text is required"})
		return
	}
	args := []string{"record", body.Text}
	if strings.TrimSpace(body.Reason) != "" {
		args = append(args, "--reason", body.Reason)
	}
	out, err := s.ggExec(r.Context(), pc.root, args...)
	writeJSONResp(w, map[string]any{"ok": err == nil, "output": out})
}

func (s *dashboardServer) handleWriteTask(w http.ResponseWriter, r *http.Request) {
	if !s.requireWrite(w, r) {
		return
	}
	pc := s.resolveOr(w, r)
	if pc == nil {
		return
	}
	var body struct{ Title, Detail string }
	if json.NewDecoder(r.Body).Decode(&body) != nil || strings.TrimSpace(body.Title) == "" {
		writeJSONResp(w, map[string]any{"error": "title is required"})
		return
	}
	args := []string{"task", "create", body.Title, "--requester", "agent"}
	if strings.TrimSpace(body.Detail) != "" {
		args = append(args, "--detail", body.Detail)
	}
	out, err := s.ggExec(r.Context(), pc.root, args...)
	writeJSONResp(w, map[string]any{"ok": err == nil, "output": out})
}

func firstN[T any](s []T, n int) []T {
	if len(s) > n {
		return s[:n]
	}
	return s
}

func openBrowser(url string) {
	var c *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		c = exec.Command("open", url)
	case "windows":
		c = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		c = exec.Command("xdg-open", url)
	}
	_ = c.Start()
}
