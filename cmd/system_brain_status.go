package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/store"
)

const systemBrainHealthNote = "gg system sync refreshes contracts/hooks plus tracker self-heal; brain status reports snapshot drift, portability checks, and CodeGraph health."

var systemBrainCmd = &cobra.Command{
	Use:   "brain",
	Short: "Cross-project brain health operations",
	Long: `Inspect brain readiness across every project in ~/.gg/projects.json.

This is intentionally separate from 'gg system sync': sync also performs
contract/hooks propagation plus tracker self-heal, while brain status verifies
project_id, backend reachability, portable brain snapshots, drift, and
CodeGraph freshness.`,
}

var systemBrainStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show per-project brain snapshot/backend/CodeGraph health",
	RunE:  runSystemBrainStatus,
}

type systemBrainStatusReport struct {
	Note     string                     `json:"note"`
	Summary  systemBrainStatusSummary   `json:"summary"`
	Projects []systemBrainProjectStatus `json:"projects"`
}

type systemBrainStatusSummary struct {
	Total           int `json:"total"`
	Healthy         int `json:"healthy"`
	Problems        int `json:"problems"`
	MissingSnapshot int `json:"missing_snapshot"`
	StaleSnapshot   int `json:"stale_snapshot"`
	DriftedSnapshot int `json:"drifted_snapshot"`
	RegistryIssues  int `json:"registry_issues"`
	BackendIssues   int `json:"backend_issues"`
	CodeGraphIssues int `json:"codegraph_issues"`
}

type systemBrainProjectStatus struct {
	Name            string                    `json:"name"`
	Root            string                    `json:"root"`
	RegistryID      string                    `json:"registry_id"`
	ConfigProjectID string                    `json:"config_project_id,omitempty"`
	RegistryStatus  string                    `json:"registry_status"`
	ProjectIDStatus string                    `json:"project_id_status"`
	Snapshot        systemBrainSnapshotStatus `json:"snapshot"`
	Qdrant          systemBrainCheck          `json:"qdrant"`
	Ollama          systemBrainCheck          `json:"ollama"`
	CodeGraph       systemBrainCodeGraph      `json:"codegraph"`
	Problems        []string                  `json:"problems,omitempty"`
}

type systemBrainCheck struct {
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

type systemBrainSnapshotStatus struct {
	Status             string         `json:"status"`
	ExportedAt         string         `json:"exported_at,omitempty"`
	Age                string         `json:"age,omitempty"`
	FreshnessThreshold string         `json:"freshness_threshold,omitempty"`
	ProjectID          string         `json:"project_id,omitempty"`
	Checksums          string         `json:"checksums,omitempty"`
	Drift              string         `json:"drift,omitempty"`
	DriftDelta         map[string]int `json:"drift_delta,omitempty"`
	Counts             map[string]int `json:"counts,omitempty"`
	Detail             string         `json:"detail,omitempty"`
}

type systemBrainCodeGraph struct {
	Status            string `json:"status"`
	Detail            string `json:"detail,omitempty"`
	MemgraphAvailable bool   `json:"memgraph_available"`
	Files             int64  `json:"files"`
	Symbols           int64  `json:"symbols"`
	Edges             int64  `json:"edges"`
	LastIndexedSHA    string `json:"last_indexed_sha,omitempty"`
	HeadSHA           string `json:"head_sha,omitempty"`
}

type systemBrainQdrantClient interface {
	brainLiveCounter
	HealthCheck(ctx context.Context) error
	Close() error
}

var systemBrainNewQdrantClient = func(cfg *config.Config, ggDir string) (systemBrainQdrantClient, error) {
	return store.New(ggDir, cfg.ProjectID)
}

var systemBrainHTTPClient = &http.Client{Timeout: 2 * time.Second}

func init() {
	systemBrainCmd.AddCommand(systemBrainStatusCmd)
	systemCmd.AddCommand(systemBrainCmd)
}

func runSystemBrainStatus(cmd *cobra.Command, _ []string) error {
	report, err := collectSystemBrainStatus(cmd.Context())
	if err != nil {
		return err
	}
	if jsonOutput {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(report)
	}
	renderSystemBrainStatus(cmd.OutOrStdout(), report)
	return nil
}

func collectSystemBrainStatus(ctx context.Context) (systemBrainStatusReport, error) {
	reg, err := config.LoadRegistry()
	if err != nil {
		return systemBrainStatusReport{}, fmt.Errorf("load registry: %w", err)
	}
	projects := reg.Sorted()
	report := systemBrainStatusReport{
		Note:     systemBrainHealthNote,
		Projects: make([]systemBrainProjectStatus, 0, len(projects)),
	}
	for _, entry := range projects {
		project := collectSystemBrainProjectStatus(ctx, entry)
		report.Projects = append(report.Projects, project)
	}
	markDuplicateSystemBrainProjectIDs(report.Projects)
	report.Summary = summarizeSystemBrainStatus(report.Projects)
	return report, nil
}

func collectSystemBrainProjectStatus(ctx context.Context, entry config.ProjectEntry) systemBrainProjectStatus {
	project := systemBrainProjectStatus{
		Name:            entry.Name,
		Root:            entry.Root,
		RegistryID:      entry.ID,
		RegistryStatus:  "ok",
		ProjectIDStatus: "unknown",
		Snapshot:        systemBrainSnapshotStatus{Status: "unknown"},
		Qdrant:          systemBrainCheck{Status: "unknown"},
		Ollama:          systemBrainCheck{Status: "unknown"},
		CodeGraph:       systemBrainCodeGraph{Status: "unknown"},
	}
	ggDir := filepath.Join(entry.Root, config.DirName)
	cfgPath := filepath.Join(ggDir, config.ConfigFile)
	if _, err := os.Stat(entry.Root); err != nil {
		project.RegistryStatus = "missing_root"
		project.ProjectIDStatus = "unknown"
		project.Snapshot = systemBrainSnapshotStatus{Status: "unknown", Detail: "project root missing"}
		project.CodeGraph = systemBrainCodeGraph{Status: "unknown", Detail: "project root missing"}
		project.addProblem("registry root missing")
		return project
	}
	if _, err := os.Stat(cfgPath); err != nil {
		project.RegistryStatus = "missing_config"
		project.ProjectIDStatus = "unknown"
		project.Snapshot = systemBrainSnapshotStatus{Status: "unknown", Detail: ".gg/config.yaml missing"}
		project.CodeGraph = systemBrainCodeGraph{Status: "unknown", Detail: ".gg/config.yaml missing"}
		project.addProblem(".gg/config.yaml missing")
		return project
	}

	cfg, err := config.LoadFromGGDir(ggDir)
	if err != nil {
		project.ProjectIDStatus = "invalid"
		project.Snapshot = systemBrainSnapshotStatus{Status: "unknown", Detail: err.Error()}
		project.CodeGraph = systemBrainCodeGraph{Status: "unknown", Detail: err.Error()}
		project.addProblem("config invalid")
		return project
	}
	project.ConfigProjectID = cfg.ProjectID
	if cfg.ProjectID == entry.ID {
		project.ProjectIDStatus = "ok"
	} else {
		project.ProjectIDStatus = "mismatch"
		project.addProblem("registry project_id mismatch")
	}

	project.Qdrant = checkSystemBrainQdrant(ctx, cfg, ggDir)
	project.Ollama = checkSystemBrainOllama(ctx, cfg)
	project.Snapshot = checkSystemBrainSnapshot(ctx, ggDir, cfg, project.Qdrant.Status == "up")
	project.CodeGraph = checkSystemBrainCodeGraph(ctx, entry.Root, ggDir, cfg)
	project.addDerivedProblems()
	return project
}

func checkSystemBrainQdrant(ctx context.Context, cfg *config.Config, ggDir string) systemBrainCheck {
	client, err := systemBrainNewQdrantClient(cfg, ggDir)
	if err != nil {
		return systemBrainCheck{Status: "down", Detail: "client init: " + err.Error()}
	}
	defer func() { _ = client.Close() }()
	hctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := client.HealthCheck(hctx); err != nil {
		return systemBrainCheck{Status: "down", Detail: compactTrim(err.Error(), 160)}
	}
	return systemBrainCheck{Status: "up", Detail: "embedded sqlite (.gg/vectorstore.db)"}
}

// checkSystemBrainOllama reports the embedding backend's health. It is
// backend-aware: under Voyage it does NOT probe the Ollama host (which would
// falsely report down for a correctly-configured Voyage user — the TASK-495
// gap); instead it checks the API-key env var. Under Ollama it probes /api/tags.
func checkSystemBrainOllama(ctx context.Context, cfg *config.Config) systemBrainCheck {
	if cfg.Embedding.ResolvedBackendName() == config.BackendVoyage {
		keyEnv := cfg.Embedding.Voyage.APIKeyEnv
		if strings.TrimSpace(keyEnv) == "" {
			keyEnv = config.DefaultVoyageAPIKeyEnv
		}
		if strings.TrimSpace(os.Getenv(keyEnv)) == "" {
			return systemBrainCheck{Status: "down", Detail: fmt.Sprintf("voyage backend: %s not set", keyEnv)}
		}
		return systemBrainCheck{Status: "up", Detail: "voyage (" + keyEnv + " present)"}
	}
	host := strings.TrimRight(cfg.Embedding.Host, "/")
	hctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(hctx, http.MethodGet, host+"/api/tags", nil)
	if err != nil {
		return systemBrainCheck{Status: "down", Detail: err.Error()}
	}
	resp, err := systemBrainHTTPClient.Do(req)
	if err != nil {
		return systemBrainCheck{Status: "down", Detail: compactTrim(err.Error(), 160)}
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return systemBrainCheck{Status: "down", Detail: fmt.Sprintf("%s returned HTTP %d", host, resp.StatusCode)}
	}
	return systemBrainCheck{Status: "up", Detail: host}
}

func checkSystemBrainSnapshot(ctx context.Context, ggDir string, cfg *config.Config, qdrantUp bool) systemBrainSnapshotStatus {
	manifestPath := filepath.Join(ggDir, brainDirName, "manifest.json")
	data, err := os.ReadFile(manifestPath) //nolint:gosec
	if err != nil {
		if os.IsNotExist(err) {
			return systemBrainSnapshotStatus{Status: "none", Detail: "run gg brain export"}
		}
		return systemBrainSnapshotStatus{Status: "unknown", Detail: err.Error()}
	}
	var manifest brainManifest
	if err := brainUnmarshalManifest(data, &manifest); err != nil {
		return systemBrainSnapshotStatus{Status: "unknown", Detail: err.Error()}
	}
	if manifest.SchemaVersion != 1 {
		return systemBrainSnapshotStatus{
			Status:    "unknown",
			ProjectID: manifest.ProjectID,
			Counts:    manifest.Counts,
			Detail:    fmt.Sprintf("unsupported snapshot schema %d", manifest.SchemaVersion),
		}
	}
	if strings.TrimSpace(manifest.ProjectID) == "" || manifest.ProjectID != cfg.ProjectID {
		return systemBrainSnapshotStatus{
			Status:    "unknown",
			ProjectID: manifest.ProjectID,
			Counts:    manifest.Counts,
			Detail:    "snapshot project_id mismatch",
		}
	}
	status := systemBrainSnapshotStatus{
		Status:     "fresh",
		ExportedAt: manifest.ExportedAt,
		ProjectID:  manifest.ProjectID,
		Checksums:  "ok",
		Drift:      "unknown",
		Counts:     manifest.Counts,
	}
	threshold, err := time.ParseDuration(strings.TrimSpace(cfg.Backup.Interval))
	if err != nil || threshold <= 0 {
		threshold = 24 * time.Hour
	}
	status.FreshnessThreshold = threshold.String()
	if exportedAt, parseErr := time.Parse(time.RFC3339, manifest.ExportedAt); parseErr == nil {
		age := time.Since(exportedAt)
		status.Age = age.Truncate(time.Second).String()
		if age >= threshold {
			status.Status = "stale"
			status.Detail = "run gg brain export"
		}
	} else {
		status.Status = "unknown"
		status.Detail = "parse exported_at: " + parseErr.Error()
	}
	if !brainManifestChecksumsOK(ggDir, manifest) {
		status.Checksums = "mismatch"
		status.Status = "unknown"
		status.Detail = "snapshot checksum mismatch"
	}
	if qdrantUp {
		client, err := systemBrainNewQdrantClient(cfg, ggDir)
		if err != nil {
			if status.Status == "fresh" {
				status.Status = "unknown"
				status.Detail = "snapshot drift unavailable: " + compactTrim(err.Error(), 160)
			}
		} else {
			defer func() { _ = client.Close() }()
			driftCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			defer cancel()
			status.Drift, status.DriftDelta = computeDrift(driftCtx, client, manifest)
			if status.Drift == "drifted" {
				status.Status = "drifted"
				status.Detail = "run gg brain export"
			} else if status.Drift == "unknown" && status.Status == "fresh" {
				status.Status = "unknown"
				status.Detail = "snapshot drift unknown"
			}
		}
	}
	return status
}

func brainManifestChecksumsOK(ggDir string, manifest brainManifest) bool {
	brainDir := filepath.Join(ggDir, brainDirName)
	for fname, expected := range manifest.SHA256 {
		if fname == "" || filepath.IsAbs(fname) || filepath.Clean(fname) != fname || filepath.Base(fname) != fname {
			return false
		}
		actual, err := fileChecksum(filepath.Join(brainDir, fname))
		if err != nil || actual != expected {
			return false
		}
	}
	return true
}

func checkSystemBrainCodeGraph(ctx context.Context, root, ggDir string, cfg *config.Config) systemBrainCodeGraph {
	graphCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	graphStatus := collectCodeGraphStatus(graphCtx, root, ggDir, cfg)
	return systemBrainCodeGraph{
		Status:            graphStatus.Status,
		Detail:            graphStatus.Detail,
		MemgraphAvailable: graphStatus.MemgraphAvailable,
		Files:             graphStatus.Stats.Files,
		Symbols:           graphStatus.Stats.Symbols,
		Edges:             graphStatus.Stats.Edges,
		LastIndexedSHA:    graphStatus.LastIndexedSHA,
		HeadSHA:           graphStatus.HeadSHA,
	}
}

func markDuplicateSystemBrainProjectIDs(projects []systemBrainProjectStatus) {
	byID := map[string]int{}
	for _, p := range projects {
		if p.ConfigProjectID != "" {
			byID[p.ConfigProjectID]++
		}
	}
	for i := range projects {
		if projects[i].ConfigProjectID != "" && byID[projects[i].ConfigProjectID] > 1 {
			projects[i].ProjectIDStatus = "duplicate"
			projects[i].addProblem("duplicate project_id across registered projects")
		}
	}
}

func summarizeSystemBrainStatus(projects []systemBrainProjectStatus) systemBrainStatusSummary {
	summary := systemBrainStatusSummary{Total: len(projects)}
	for _, p := range projects {
		if len(p.Problems) == 0 {
			summary.Healthy++
		} else {
			summary.Problems++
		}
		if p.RegistryStatus != "ok" || p.ProjectIDStatus != "ok" {
			summary.RegistryIssues++
		}
		switch p.Snapshot.Status {
		case "none":
			summary.MissingSnapshot++
		case "stale":
			summary.StaleSnapshot++
		case "drifted":
			summary.DriftedSnapshot++
		}
		if p.Qdrant.Status != "up" || p.Ollama.Status != "up" {
			summary.BackendIssues++
		}
		if p.CodeGraph.Status != "ready" && p.CodeGraph.Status != "not_applicable" {
			summary.CodeGraphIssues++
		}
	}
	return summary
}

func (p *systemBrainProjectStatus) addDerivedProblems() {
	if p.Snapshot.Status != "fresh" {
		p.addProblem("snapshot=" + p.Snapshot.Status)
	}
	if p.Qdrant.Status != "up" {
		p.addProblem("qdrant=" + p.Qdrant.Status)
	}
	if p.Ollama.Status != "up" {
		p.addProblem("ollama=" + p.Ollama.Status)
	}
	if p.CodeGraph.Status != "ready" && p.CodeGraph.Status != "not_applicable" {
		p.addProblem("codegraph=" + p.CodeGraph.Status)
	}
}

func (p *systemBrainProjectStatus) addProblem(problem string) {
	for _, existing := range p.Problems {
		if existing == problem {
			return
		}
	}
	p.Problems = append(p.Problems, problem)
}

func renderSystemBrainStatus(w io.Writer, report systemBrainStatusReport) {
	fmt.Fprintf(w, "Brain health across %d registered project(s)\n", report.Summary.Total)
	fmt.Fprintf(w, "Note: %s\n", report.Note)
	fmt.Fprintln(w, "──────────────────────────────────────────────────")
	if len(report.Projects) == 0 {
		fmt.Fprintln(w, "No projects registered. Run `gg init` in a project or `gg system register --path /path/to/project`.")
		return
	}
	for _, p := range report.Projects {
		fmt.Fprintf(w, "• %-20s  %s\n", p.Name, p.Root)
		fmt.Fprintf(w, "  registry=%s project_id=%s snapshot=%s qdrant=%s ollama=%s codegraph=%s\n",
			p.RegistryStatus, p.ProjectIDStatus, p.Snapshot.Status, p.Qdrant.Status, p.Ollama.Status, p.CodeGraph.Status)
		if len(p.Problems) > 0 {
			fmt.Fprintf(w, "  problems: %s\n", strings.Join(p.Problems, "; "))
		}
	}
	fmt.Fprintln(w, "──────────────────────────────────────────────────")
	fmt.Fprintf(w, "healthy=%d problems=%d registry_issues=%d missing_snapshot=%d stale_snapshot=%d drifted_snapshot=%d backend_issues=%d codegraph_issues=%d\n",
		report.Summary.Healthy, report.Summary.Problems, report.Summary.RegistryIssues,
		report.Summary.MissingSnapshot, report.Summary.StaleSnapshot, report.Summary.DriftedSnapshot,
		report.Summary.BackendIssues, report.Summary.CodeGraphIssues)
}
