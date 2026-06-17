package cmd

import (
	"context"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/dashboard"
	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/embedding"
	"github.com/gurkangul/gg-cli/internal/store"
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
	client, err := store.New(ggDir, id)
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
