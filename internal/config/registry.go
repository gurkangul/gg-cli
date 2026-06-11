package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// RegistryFile is the name of the project registry inside ~/.gg/. Maps every
// project_id a `gg init` has touched on this host to its filesystem root so
// cross-project commands (gg system sync, gg system contract-check --all) can
// iterate without scanning the whole home directory.
const RegistryFile = "projects.json"

// ProjectEntry is a single record in the registry. Name is derived from the
// root path's basename at registration time — purely cosmetic, not a key.
type ProjectEntry struct {
	ID           string `json:"id"`
	Root         string `json:"root"`
	Name         string `json:"name"`
	RegisteredAt string `json:"registered_at"`
}

// Registry is the root structure serialized to ~/.gg/projects.json.
// Projects is a map keyed by project_id so duplicate init calls overwrite
// rather than duplicate.
type Registry struct {
	// SchemaVersion stamps the projects.json format so a future shape change
	// is detectable. Absent/zero in an older registry is treated as the
	// current baseline — it never fails the load (docs/stability.md §3).
	// Save stamps CurrentRegistrySchemaVersion; a value higher than this
	// binary understands warns (LoadRegistry) but still loads.
	SchemaVersion int                     `json:"schema_version,omitempty"`
	Projects      map[string]ProjectEntry `json:"projects"`
}

// registryPath returns the absolute location of projects.json.
func registryPath() (string, error) {
	sd, err := SharedDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(sd, RegistryFile), nil
}

// LoadRegistry reads projects.json. A missing file is not an error — it
// returns an empty (but non-nil) registry so callers can treat first use and
// every subsequent use identically.
func LoadRegistry() (*Registry, error) {
	p, err := registryPath()
	if err != nil {
		return nil, err
	}
	reg := &Registry{Projects: map[string]ProjectEntry{}}
	b, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return reg, nil
		}
		return nil, fmt.Errorf("read %s: %w", p, err)
	}
	if err := json.Unmarshal(b, reg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", p, err)
	}
	if reg.Projects == nil {
		reg.Projects = map[string]ProjectEntry{}
	}
	// Forward-only readability (§3): a registry written by a newer gg loads
	// fine, but we flag it once on stderr so an ignored future field is not
	// silently surprising. A missing/zero stamp is an older baseline — no warn.
	warnRegistryAhead(reg.SchemaVersion, RegistryFile)
	return reg, nil
}

// Save writes the registry back atomically (tmp + rename) so a crashed write
// can never leave the file in a half-parsed state.
func (r *Registry) Save() error {
	p, err := registryPath()
	if err != nil {
		return err
	}
	// Stamp the current schema version on every write so newly-written
	// registries are versioned. Never downgrade a higher (future) stamp.
	if r.SchemaVersion < CurrentRegistrySchemaVersion {
		r.SchemaVersion = CurrentRegistrySchemaVersion
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return fmt.Errorf("create registry dir: %w", err)
	}
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal registry: %w", err)
	}
	b = append(b, '\n')
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, p); err != nil {
		return fmt.Errorf("rename %s → %s: %w", tmp, p, err)
	}
	return nil
}

// Add inserts or updates a project entry. A second call with the same
// project_id overwrites (covers both `gg init` reruns and `gg init` on a
// project whose root directory moved).
func (r *Registry) Add(id, root string) {
	r.Projects[id] = ProjectEntry{
		ID:           id,
		Root:         root,
		Name:         filepath.Base(root),
		RegisteredAt: time.Now().UTC().Format(time.RFC3339),
	}
}

// Remove drops an entry by project_id. Returns true if something was removed
// so callers can distinguish "pruned" from "wasn't there".
func (r *Registry) Remove(id string) bool {
	if _, ok := r.Projects[id]; !ok {
		return false
	}
	delete(r.Projects, id)
	return true
}

// Sorted returns registry entries in stable order (by Name, then ID) so CLI
// output and iteration are reproducible across runs. Callers that mutate must
// go back through Add/Remove.
func (r *Registry) Sorted() []ProjectEntry {
	out := make([]ProjectEntry, 0, len(r.Projects))
	for _, p := range r.Projects {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].ID < out[j].ID
	})
	return out
}
