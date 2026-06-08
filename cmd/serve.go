package cmd

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
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
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/store"
	"github.com/gurkangul/gg-cli/internal/telemetry"
)

//go:embed dashboard.html
var dashboardHTML []byte

var (
	servePort   int
	serveNoOpen bool
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Local dashboard — visualize the project brain (decisions, work, live search)",
	Long: `gg serve starts a FOREGROUND, localhost-only web dashboard for this project's brain.

It is NOT a daemon: it runs only until you press Ctrl-C, binds to 127.0.0.1
exclusively (no network exposure), and serves the same JSONL/Qdrant stores the
CLI reads. Anyone who ran 'gg init' can open it with 'gg serve'.

  gg serve                # open the dashboard at http://127.0.0.1:7777
  gg serve --port 8080    # pick another port
  gg serve --no-open      # do not auto-open the browser`,
	RunE: runServe,
}

func init() {
	serveCmd.Flags().IntVar(&servePort, "port", 7777, "localhost port to bind")
	serveCmd.Flags().BoolVar(&serveNoOpen, "no-open", false, "do not open the browser automatically")
	rootCmd.AddCommand(serveCmd)
}

func runServe(cmd *cobra.Command, _ []string) error {
	// Read-only deps with an embedder so the live search playground works.
	d, err := loadDepsReadOnly(true)
	if err != nil {
		return err
	}
	cfg, _ := config.Load()
	srv := &dashboardServer{d: d, cfg: cfg}

	mux := http.NewServeMux()
	mux.HandleFunc("/", srv.handleIndex)
	mux.HandleFunc("/api/overview", srv.handleOverview)
	mux.HandleFunc("/api/search", srv.handleSearch)
	mux.HandleFunc("/api/decisions", srv.handleDecisions)
	mux.HandleFunc("/api/tasks", srv.handleTasks)
	mux.HandleFunc("/api/bugs", srv.handleBugs)
	mux.HandleFunc("/api/telemetry", srv.handleTelemetry)
	mux.HandleFunc("/api/files", srv.handleFiles)
	mux.HandleFunc("/api/file", srv.handleFile)

	addr := fmt.Sprintf("127.0.0.1:%d", servePort)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		d.Close()
		return fmt.Errorf("cannot bind %s: %w (try --port)", addr, err)
	}
	url := "http://" + addr
	httpSrv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}

	fmt.Printf("─── gg dashboard ───\n")
	fmt.Printf("  %s\n", url)
	fmt.Printf("  localhost-only, foreground (Ctrl-C to stop) — not a daemon\n")
	if d.qdrantDown {
		fmt.Println("  ⚠ Qdrant unreachable — showing JSONL data; live search disabled")
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
		d.Close()
		return err
	}
	shutCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(shutCtx)
	d.Close()
	return nil
}

type dashboardServer struct {
	d   *deps
	cfg *config.Config
}

func (s *dashboardServer) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(dashboardHTML)
}

func writeJSONResp(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (s *dashboardServer) handleOverview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	decs, _ := s.d.store.ListDecisions(ctx, 0, false)
	rejs, _ := s.d.store.ListRejections(ctx, 0)
	bugs, _ := s.d.store.ListBugs(ctx, "")
	tasks, _ := s.d.store.ListTasks(ctx, "")
	fixed, _ := s.d.store.ListBugs(ctx, "fixed")

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
		"project": projectLabel(s.cfg),
		"counts": map[string]int{
			"decisions": len(decs), "rejections": len(rejs),
			"bugs": len(bugs), "bugsOpen": openBugs, "bugsFixed": len(fixed),
			"tasks": len(tasks), "tasksDone": doneTasks, "tasksOpen": openTasks,
		},
		"canon":           store.BuildAutoCanon(decs, rejs, fixed),
		"recentDecisions": firstN(store.FilterDecisionNoise(decs), 25),
	})
}

func (s *dashboardServer) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeJSONResp(w, map[string]any{"error": "empty query"})
		return
	}
	if s.d.qdrantDown || s.d.embedder == nil {
		writeJSONResp(w, map[string]any{"error": "live search needs Qdrant + Ollama; stores are degraded"})
		return
	}
	ctx := r.Context()
	t0 := time.Now()
	vec, err := s.d.embedder.Generate(ctx, q)
	embedMs := time.Since(t0).Milliseconds()
	if err != nil {
		writeJSONResp(w, map[string]any{"error": "embed failed: " + err.Error()})
		return
	}
	t1 := time.Now()
	decs, _ := s.d.store.SearchDecisions(ctx, vec, 8, false)
	rejs, _ := s.d.store.SearchRejections(ctx, vec, 5)
	bugs, _ := s.d.store.SearchBugs(ctx, vec, 5, false)
	searchMs := time.Since(t1).Milliseconds()
	writeJSONResp(w, map[string]any{
		"query": q, "vectorDim": len(vec),
		"embedMs": embedMs, "searchMs": searchMs,
		"decisions": decs, "rejections": rejs, "bugs": bugs,
	})
}

func (s *dashboardServer) handleDecisions(w http.ResponseWriter, r *http.Request) {
	decs, _ := s.d.store.ListDecisions(r.Context(), 0, false)
	writeJSONResp(w, store.FilterDecisionNoise(decs))
}

func (s *dashboardServer) handleTasks(w http.ResponseWriter, r *http.Request) {
	tasks, _ := s.d.store.ListTasks(r.Context(), r.URL.Query().Get("status"))
	writeJSONResp(w, tasks)
}

func (s *dashboardServer) handleBugs(w http.ResponseWriter, r *http.Request) {
	bugs, _ := s.d.store.ListBugs(r.Context(), r.URL.Query().Get("status"))
	writeJSONResp(w, bugs)
}

func (s *dashboardServer) handleTelemetry(w http.ResponseWriter, r *http.Request) {
	if s.cfg == nil {
		writeJSONResp(w, map[string]any{})
		return
	}
	rtDir, err := s.cfg.RuntimeDir()
	if err != nil {
		writeJSONResp(w, map[string]any{"error": err.Error()})
		return
	}
	wk, _ := telemetry.Summarize(rtDir)
	sess, _ := telemetry.SummarizeSessions(rtDir, time.Now().UTC().AddDate(0, 0, -7))
	writeJSONResp(w, map[string]any{"weekly": wk, "sessions": sess})
}

var brainFileName = regexp.MustCompile(`^[a-z][a-z-]*$`)

// handleFiles lists the JSONL source-of-truth files so the dashboard can show
// where the brain physically lives and how big each store is.
func (s *dashboardServer) handleFiles(w http.ResponseWriter, _ *http.Request) {
	ggDir, err := config.GGDir()
	if err != nil {
		writeJSONResp(w, map[string]any{"error": err.Error()})
		return
	}
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
	ggDir, err := config.GGDir()
	if err != nil {
		writeJSONResp(w, map[string]any{"error": err.Error()})
		return
	}
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

func projectLabel(cfg *config.Config) string {
	if cfg == nil || cfg.ProjectID == "" {
		return "gg project"
	}
	return cfg.ProjectID
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
