package cmd

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/store"
)

type sourceLabels map[string]string

func sourceKey(kind, id string) string {
	return kind + ":" + id
}

func (s sourceLabels) set(kind, id, projectID string) {
	if s != nil && id != "" && projectID != "" {
		s[sourceKey(kind, id)] = projectID
	}
}

func (s sourceLabels) get(kind, id string) string {
	if s == nil {
		return ""
	}
	return s[sourceKey(kind, id)]
}

func labelSearchResults(results []searchResult, projectID string) []searchResult {
	for i := range results {
		results[i].SourceProjectID = projectID
	}
	return results
}

func markBundleSource(bundle *contextBundle, projectID string) {
	if bundle.sources == nil {
		bundle.sources = sourceLabels{}
	}
	for _, d := range bundle.decisions {
		bundle.sources.set("decision", d.ID, projectID)
	}
	for _, r := range bundle.rejections {
		bundle.sources.set("rejection", r.ID, projectID)
	}
	for _, t := range bundle.tasks {
		bundle.sources.set("task", t.ID, projectID)
	}
	for _, d := range bundle.discussions {
		bundle.sources.set("discussion", d.ID, projectID)
	}
	for _, n := range bundle.notes {
		bundle.sources.set("note", n.ID, projectID)
	}
}

func sourcePrefix(projectID string) string {
	if projectID == "" {
		return ""
	}
	return "[" + projectID + "] "
}

func sourceSuffix(projectID string) string {
	if projectID == "" {
		return ""
	}
	return "  @" + projectID
}

type linkedStore struct {
	projectID string
	client    *store.Client
}

func openLinkedStores(cfg *config.Config) ([]linkedStore, []string) {
	var stores []linkedStore
	var warnings []string
	for _, ref := range cfg.LinkedProjects {
		projectID, ggDir, err := config.ResolveLinkedProject(ref)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("linked project %s: %v", linkedRefLabel(ref), err))
			continue
		}
		if projectID == cfg.ProjectID {
			continue
		}
		if ggDir == "" {
			ggDir = config.GGDirOrEmpty()
		}
		client, err := store.New(&cfg.Qdrant, ggDir, projectID)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("linked project %s: %v", projectID, err))
			continue
		}
		stores = append(stores, linkedStore{projectID: projectID, client: client})
	}
	return stores, warnings
}

func closeLinkedStores(stores []linkedStore) {
	for _, linked := range stores {
		_ = linked.client.Close()
	}
}

func linkedRefLabel(ref config.LinkedProjectConfig) string {
	if strings.TrimSpace(ref.ProjectID) != "" {
		return strings.TrimSpace(ref.ProjectID)
	}
	if strings.TrimSpace(ref.Path) != "" {
		return strings.TrimSpace(ref.Path)
	}
	return "<empty>"
}

func linkedSearchResults(ctx context.Context, cfg *config.Config, query string, vector []float32, semanticLimit uint64) ([]searchResult, []string) {
	stores, warnings := openLinkedStores(cfg)
	defer closeLinkedStores(stores)

	var mu sync.Mutex
	var out []searchResult
	var wg sync.WaitGroup
	for _, linked := range stores {
		linked := linked
		wg.Add(1)
		go func() {
			defer wg.Done()
			results, errs := searchResultsForProject(ctx, linked.client, query, vector, semanticLimit, linked.projectID)
			mu.Lock()
			out = append(out, results...)
			for _, err := range errs {
				warnings = append(warnings, fmt.Sprintf("linked project %s: %s", linked.projectID, err))
			}
			mu.Unlock()
		}()
	}
	wg.Wait()
	return out, warnings
}

func searchResultsForProject(ctx context.Context, client *store.Client, query string, vector []float32, limit uint64, projectID string) ([]searchResult, []string) {
	var decisions []store.Decision
	var rejections []store.Rejection
	var tasks []store.Task
	var bugs []store.Bug
	var notes []store.Note
	var decErr, rejErr, taskErr, bugErr, noteErr error
	var wg sync.WaitGroup
	wg.Add(5)
	go func() { defer wg.Done(); decisions, decErr = client.SearchDecisions(ctx, vector, limit, false) }()
	go func() { defer wg.Done(); rejections, rejErr = client.SearchRejections(ctx, vector, limit) }()
	go func() { defer wg.Done(); tasks, taskErr = client.SearchTasks(ctx, vector, limit, false) }()
	go func() { defer wg.Done(); bugs, bugErr = client.SearchBugs(ctx, vector, limit, false) }()
	go func() { defer wg.Done(); notes, noteErr = client.SearchNotes(ctx, vector, limit) }()
	wg.Wait()

	var errs []string
	for name, err := range map[string]error{
		"decisions": decErr, "rejections": rejErr, "tasks": taskErr, "bugs": bugErr, "notes": noteErr,
	} {
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", name, err))
		}
	}
	if len(errs) > 0 && len(decisions)+len(rejections)+len(tasks)+len(bugs)+len(notes) == 0 {
		return nil, errs
	}
	return labelSearchResults(buildSearchResults(query, decisions, rejections, tasks, bugs, notes), projectID), errs
}

func appendLinkedContext(ctx context.Context, cfg *config.Config, bundle *contextBundle, vector []float32) []string {
	stores, warnings := openLinkedStores(cfg)
	defer closeLinkedStores(stores)
	for _, linked := range stores {
		linkedBundle, errs := contextBundleForProject(ctx, linked.client, vector, linked.projectID)
		warnings = append(warnings, errs...)
		bundle.decisions = append(bundle.decisions, linkedBundle.decisions...)
		bundle.rejections = append(bundle.rejections, linkedBundle.rejections...)
		bundle.tasks = append(bundle.tasks, linkedBundle.tasks...)
		bundle.discussions = append(bundle.discussions, linkedBundle.discussions...)
		bundle.notes = append(bundle.notes, linkedBundle.notes...)
		if bundle.sources == nil {
			bundle.sources = sourceLabels{}
		}
		for k, v := range linkedBundle.sources {
			bundle.sources[k] = v
		}
	}
	return warnings
}

func contextBundleForProject(ctx context.Context, client *store.Client, vector []float32, projectID string) (contextBundle, []string) {
	var bundle contextBundle
	var wg sync.WaitGroup
	wg.Add(5)
	go func() {
		defer wg.Done()
		bundle.decisions, bundle.decErr = client.SearchDecisions(ctx, vector, contextLimit, false)
	}()
	go func() {
		defer wg.Done()
		bundle.rejections, bundle.rejErr = client.SearchRejections(ctx, vector, contextLimit)
	}()
	go func() {
		defer wg.Done()
		bundle.tasks, bundle.taskErr = client.SearchTasks(ctx, vector, contextLimit, contextIncludeResolved)
	}()
	go func() {
		defer wg.Done()
		bundle.discussions, bundle.discErr = client.SearchDiscussions(ctx, vector, contextLimit, contextIncludeResolved)
	}()
	go func() { defer wg.Done(); bundle.notes, bundle.noteErr = client.SearchNotes(ctx, vector, contextLimit) }()
	wg.Wait()
	errs := collectBundleErrors(bundle)
	for i, err := range errs {
		errs[i] = fmt.Sprintf("linked project %s: %s", projectID, err)
	}
	markBundleSource(&bundle, projectID)
	return bundle, errs
}
