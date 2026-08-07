package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/embedding"
	"github.com/gurkangul/gg-cli/internal/store"
)

// callTimeout bounds a single tool invocation so a slow/unavailable embedding
// backend can never wedge the stdio loop.
const callTimeout = 15 * time.Second

// defaultLimit is the per-collection result cap for search/context tools.
const defaultLimit = 5

// Host is the read-only ToolHost backing the gg MCP server. It resolves the
// project brain lazily on each call from the configured project directory and
// closes it when the call returns, so a long-lived server process always reads
// the current on-disk state and only holds a store handle for the duration of a
// single tool call. The read-only guarantee is enforced by the tool surface
// (only gg_* read tools are registered — no write path exists), not by the file
// handle: the underlying SQLite/store open may take a writable handle.
//
// Project resolution failure (no .gg in the resolved directory) is NOT fatal:
// initialize still succeeds so the client connects, and tools/call returns an
// isError content block telling the user to run gg init / cd into a gg project.
type Host struct{}

// NewHost constructs the read-only tool host. The project is resolved per-call
// from the process CWD (which `gg mcp serve --project <path>` may have already
// chdir'd into), so nothing is captured here.
func NewHost() *Host { return &Host{} }

// brain bundles the per-call read dependencies: the vector store client, an
// embedder for semantic queries, the loaded config, and the resolved .gg dir.
type brain struct {
	store    *store.Client
	embedder *embedding.Generator
	cfg      *config.Config
	ggDir    string
}

func (b *brain) close() {
	if b != nil && b.store != nil {
		_ = b.store.Close()
	}
}

// openBrain resolves the project from CWD and opens the read-only store +
// embedder. It mirrors cmd.loadDepsReadOnly without importing cmd (which would
// create an import cycle): same config.Load → CheckMeta → store.New → embedder
// sequence. A returned error means the project could not be resolved/opened and
// should surface as an isError tool result, not a protocol crash.
func openBrain() (*brain, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	ggDir, err := config.GGDir()
	if err != nil {
		return nil, err
	}
	// Resolve the authoritative dim once and use it for BOTH the meta guard and the
	// embedder, so an off-default-dim Ollama model can't trip a false ErrModelMismatch here.
	dimCtx, dimCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer dimCancel()
	dim := embedding.EffectiveDim(dimCtx, &cfg.Embedding, ggDir, store.VectorSize)
	// Only validate against existing meta — don't write on first run from a search
	// call, because a probe failure here would stamp the default's dim even for a
	// model of a different size (the first-run write belongs to `gg reembed`).
	if m, _ := embedding.ReadMeta(ggDir); m != nil {
		if metaErr := embedding.CheckMeta(ggDir, embedding.EffectiveModelIdentity(&cfg.Embedding), dim); metaErr != nil {
			return nil, metaErr
		}
	}
	client, err := store.New(ggDir, cfg.ProjectID)
	if err != nil {
		return nil, err
	}
	return &brain{
		store:    client,
		embedder: embedding.New(&cfg.Embedding, dim),
		cfg:      cfg,
		ggDir:    ggDir,
	}, nil
}

// objSchema builds a JSON-Schema object descriptor with the given properties
// and required fields. Property entries are {name: {type, description}}.
func objSchema(props map[string]map[string]any, required ...string) map[string]any {
	schema := map[string]any{
		"type":       "object",
		"properties": toAnyMap(props),
	}
	if len(required) > 0 {
		schema["required"] = toAnySlice(required)
	}
	return schema
}

func toAnyMap(props map[string]map[string]any) map[string]any {
	out := make(map[string]any, len(props))
	for k, v := range props {
		out[k] = v
	}
	return out
}

func toAnySlice(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

func strProp(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}
func boolProp(desc string) map[string]any {
	return map[string]any{"type": "boolean", "description": desc}
}
func intProp(desc string) map[string]any {
	return map[string]any{"type": "integer", "description": desc}
}

// ListTools advertises the six read-only gg_* tools. No write tool exists.
func (h *Host) ListTools() []Tool {
	return []Tool{
		{
			Name:        "gg_search",
			Description: "Semantic search across the project brain (decisions, rejections, tasks, bugs, notes). Run before changing behavior to surface prior decisions and REJECTED approaches — what was already tried and ruled out, which grep/Read cannot show.",
			InputSchema: objSchema(map[string]map[string]any{
				"query":   strProp("the search query"),
				"compact": boolProp("also include a per-collection counts map for a quick size estimate"),
			}, "query"),
		},
		{
			Name:        "gg_context",
			Description: "Unified context bundle. With for_task: the task, its dependencies, and related decisions/rejections. With query: a topic bundle. With neither: a project onboarding overview.",
			InputSchema: objSchema(map[string]map[string]any{
				"query":    strProp("topic to build a context bundle for"),
				"for_task": strProp("TASK-NNN for task-scoped rehydration"),
				"compact":  boolProp("also include a per-collection counts map for a quick size estimate"),
			}),
		},
		{
			Name:        "gg_impact",
			Description: "Find every file affected by changing a file (or a BUG-NNN / TASK-NNN). Use INSTEAD of grep/ripgrep for \"what depends on X\": it follows the real import graph and catches re-exports/barrels and aliased imports that text search misses, plus related decisions, tasks, and bugs. Call before editing a shared or exported file. (For the exact callers of one symbol, use gg_def then `gg lsp refs`.)",
			InputSchema: objSchema(map[string]map[string]any{
				"target": strProp("source file path, BUG-NNN, or TASK-NNN"),
				"hops":   intProp("downstream dependency hops to traverse in file mode (default 1)"),
			}, "target"),
		},
		{
			Name:        "gg_def",
			Description: "Find where a symbol is defined — returns the file(s) and kind for a symbol name. Use INSTEAD of grep for \"where is X defined\". Graph-backed and offline; complements gg_impact.",
			InputSchema: objSchema(map[string]map[string]any{
				"name": strProp("the symbol name, e.g. DependentsOf"),
			}, "name"),
		},
		{
			Name:        "gg_uses",
			Description: "Find which files USE (reference) a symbol — the symbol-exact \"who uses X\". Use INSTEAD of grep before changing or deleting a shared/exported symbol: it matches the specific symbol, so a barrel (export *) re-export never over-reports consumers of sibling symbols the way file-level impact does. Pass file to disambiguate a name defined in multiple files.",
			InputSchema: objSchema(map[string]map[string]any{
				"name": strProp("the symbol name, e.g. CollapsePanel"),
				"file": strProp("optional defining source file, to disambiguate a name defined in multiple files"),
			}, "name"),
		},
		{
			Name:        "gg_canon",
			Description: "The project canon: distilled institutional memory plus an auto-derived live digest of the ledger. Read at session start.",
			InputSchema: objSchema(map[string]map[string]any{
				"compact": boolProp("one line per canon area to preserve context window"),
			}),
		},
		{
			Name:        "gg_task_get",
			Description: "Fetch a single task by id (TASK-NNN) with its full detail, status, and metadata.",
			InputSchema: objSchema(map[string]map[string]any{
				"id": strProp("the task id, e.g. TASK-501"),
			}, "id"),
		},
		{
			Name:        "gg_bug_get",
			Description: "Fetch a single bug by id (BUG-NNN) with its full detail, severity, status, and root cause.",
			InputSchema: objSchema(map[string]map[string]any{
				"id": strProp("the bug id, e.g. BUG-084"),
			}, "id"),
		},
	}
}

// CallTool dispatches a read-only tool. Unknown tool names and project
// resolution failures return an isError content block rather than a protocol
// error, so the client stays connected and can surface the message.
func (h *Host) CallTool(ctx context.Context, name string, args map[string]any) ([]ContentBlock, bool) {
	cctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()

	b, err := openBrain()
	if err != nil {
		return TextBlock("project brain unavailable: " + err.Error() +
			"\nRun 'gg init' in this project, or start the server with --project <path> pointing at a gg project (a directory containing a .gg/ folder)."), true
	}
	defer b.close()

	switch name {
	case "gg_search":
		return b.toolSearch(cctx, args)
	case "gg_context":
		return b.toolContext(cctx, args)
	case "gg_impact":
		return b.toolImpact(cctx, args)
	case "gg_def":
		return b.toolDef(cctx, args)
	case "gg_uses":
		return b.toolUses(cctx, args)
	case "gg_canon":
		return b.toolCanon(cctx, args)
	case "gg_task_get":
		return b.toolTaskGet(cctx, args)
	case "gg_bug_get":
		return b.toolBugGet(cctx, args)
	default:
		return TextBlock("unknown tool: " + name), true
	}
}

// ── argument helpers ──────────────────────────────────────────────────────

func argString(args map[string]any, key string) string {
	if v, ok := args[key]; ok {
		if s, ok := v.(string); ok {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

func argBool(args map[string]any, key string) bool {
	if v, ok := args[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return false
}

func argInt(args map[string]any, key string, def int) int {
	if v, ok := args[key]; ok {
		switch n := v.(type) {
		case float64: // JSON numbers decode to float64
			return int(n)
		case int:
			return n
		}
	}
	return def
}

// jsonContent marshals v as indented JSON and wraps it as a text block. MCP
// content is text; structured payloads travel as a JSON string the agent reads.
func jsonContent(v any) []ContentBlock {
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return TextBlock(fmt.Sprintf("encode error: %v", err))
	}
	return TextBlock(string(out))
}
