package cmd

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/gurkangul/gg-cli/internal/store"
	"github.com/gurkangul/gg-cli/internal/telemetry"
)

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
