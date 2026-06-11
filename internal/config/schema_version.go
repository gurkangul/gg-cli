package config

import (
	"fmt"
	"io"
	"os"
	"reflect"
	"sort"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// Schema-version stamps for gg's two unversioned on-disk formats. Both honour
// the 1.0 storage contract (docs/stability.md §3 forward-only readability, §5
// additive-keys rule): an older file without the stamp loads as the baseline
// version with no error; a file whose stamp is *higher* than the running
// binary understands loads but emits a single stderr warning so a future
// shape change is detectable rather than silently mis-read.
const (
	// CurrentConfigSchemaVersion is the schema version stamped into
	// .gg/config.yaml by ApplyDefaults. Absent/zero in an older config is
	// treated as this baseline — never a load failure.
	CurrentConfigSchemaVersion = 1

	// CurrentRegistrySchemaVersion is the schema version stamped into
	// ~/.gg/projects.json on Save. Absent/zero in an older registry is
	// treated as this baseline — never a load failure.
	CurrentRegistrySchemaVersion = 1
)

// warnSink is where config-layer deprecation/forward-compat warnings are
// written. It defaults to stderr (so stdout and --json output stay clean) and
// is overridable in tests. Warnings are advisory only — they never change the
// load result.
var warnSink io.Writer = os.Stderr

// warnedOnce dedupes identical config-layer warnings within a single process.
// A command like `gg status` may call LoadFromGGDir many times for one project;
// without this guard the same unrecognised-key notice would repeat per load,
// spamming stderr. Keyed by the full formatted message so distinct files/keys
// still each surface exactly once. Tests reset it via resetWarnedOnce.
var (
	warnedMu   sync.Mutex
	warnedOnce = map[string]struct{}{}
)

// emitWarnOnce writes msg to warnSink only the first time this exact message is
// seen in the process. Returns true if it actually emitted.
func emitWarnOnce(msg string) bool {
	warnedMu.Lock()
	defer warnedMu.Unlock()
	if _, seen := warnedOnce[msg]; seen {
		return false
	}
	warnedOnce[msg] = struct{}{}
	fmt.Fprint(warnSink, msg)
	return true
}

// resetWarnedOnce clears the dedupe cache (test-only).
func resetWarnedOnce() {
	warnedMu.Lock()
	defer warnedMu.Unlock()
	warnedOnce = map[string]struct{}{}
}

// knownConfigKeys is the set of top-level YAML keys the Config struct
// recognises. It is derived once from the struct's yaml tags so it cannot
// drift from the actual fields. Keys present in a config.yaml but absent here
// are reported as unrecognised (removed/renamed?) — they still load, ignored.
var knownConfigKeys = collectYAMLKeys(reflect.TypeOf(Config{}))

// collectYAMLKeys returns the lowercased top-level yaml key for every exported
// field of rt, following the same rules go-yaml uses: an explicit
// `yaml:"name"` tag wins (its first comma-separated segment), otherwise the
// field name is lowercased. Fields tagged `yaml:"-"` are skipped.
func collectYAMLKeys(rt reflect.Type) map[string]struct{} {
	out := map[string]struct{}{}
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		if f.PkgPath != "" { // unexported
			continue
		}
		name := f.Name
		if tag := f.Tag.Get("yaml"); tag != "" {
			seg := strings.SplitN(tag, ",", 2)[0]
			if seg == "-" {
				continue
			}
			if seg != "" {
				name = seg
			}
		}
		out[strings.ToLower(name)] = struct{}{}
	}
	return out
}

// warnUnknownConfigKeys decodes raw config bytes into a generic map, diffs the
// top-level keys against the keys the Config struct recognises, and emits ONE
// concise stderr warning per load naming any unrecognised keys. It never
// returns an error: an unknown key is a non-fatal deprecation signal, not a
// load failure (docs/stability.md §5). label is the path shown to the user.
func warnUnknownConfigKeys(data []byte, label string) {
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return // malformed YAML is reported by the real decode, not here
	}
	var unknown []string
	for k := range raw {
		if _, ok := knownConfigKeys[strings.ToLower(k)]; !ok {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) == 0 {
		return
	}
	sort.Strings(unknown)
	emitWarnOnce(fmt.Sprintf(
		"warning: %s has unrecognized key(s): %s — ignored (removed/renamed?)\n",
		label, strings.Join(unknown, ", ")))
}

// warnRegistryAhead emits a single stderr warning when a loaded registry's
// SchemaVersion is newer than this binary understands. The registry still
// loads (forward-only readability, §3); the warning flags that a newer gg may
// have written fields this binary will ignore.
func warnRegistryAhead(got int, label string) {
	if got > CurrentRegistrySchemaVersion {
		emitWarnOnce(fmt.Sprintf(
			"warning: %s schema_version %d is newer than this gg understands (%d) — loading anyway; some fields may be ignored\n",
			label, got, CurrentRegistrySchemaVersion))
	}
}
