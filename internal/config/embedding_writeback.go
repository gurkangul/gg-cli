package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// embedding_writeback.go — BUG-096: keep config.yaml in step with the corpus.
//
// gg reembed probes the configured model, rebuilds every collection at its
// dimension and rewrites embedding-meta.json — but it used to leave
// embedding.model in config.yaml pointing at the OLD model. The two then
// disagree permanently and nothing reconciles them, so every later command sees
// a project whose config asserts a model it does not use. Measured across one
// host, 8 of 11 registered projects had drifted this way.
//
// Save() cannot be used for this: it round-trips the struct through
// yaml.Marshal, which silently discards every comment in the file — including
// the install guidance gg itself writes at init. So the update is a targeted
// line rewrite that preserves layout and comments.
//
// Only the `model:` key that is a DIRECT child of `embedding:` is touched.
// embedding.voyage.model is nested deeper and is a different setting; rewriting
// it here would repoint the cloud backend as a side effect of an Ollama
// migration.

// dimClaimPattern matches a dimension assertion in a trailing comment, e.g.
// "# 768-dim — matches the vector store size". Carrying that comment across a
// model change verbatim would leave the file asserting something false, since
// the whole reason for the change is usually a different vector size.
var dimClaimPattern = regexp.MustCompile(`\b\d{3,5}-dim\b`)

// UpdateEmbeddingModel rewrites embedding.model in <ggDir>/config.yaml to model,
// preserving indentation and comments, and refreshing any "<N>-dim" claim in the
// trailing comment to dim (pass dim <= 0 to leave the comment's number alone).
//
// Returns changed=false with no error when the file already says model, when
// there is no embedding.model key, or when config.yaml does not exist — callers
// treat this as best-effort and must not fail their operation on it.
func UpdateEmbeddingModel(ggDir, model string, dim int) (changed bool, err error) {
	model = strings.TrimSpace(model)
	if model == "" {
		return false, nil
	}
	path := filepath.Join(ggDir, ConfigFile)
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("read config: %w", err)
	}

	lines := strings.SplitAfter(string(raw), "\n")
	idx, indent := embeddingModelLine(lines)
	if idx < 0 {
		return false, nil
	}

	key, value, comment := splitYAMLLine(lines[idx])
	if strings.TrimSpace(value) == model {
		return false, nil
	}
	if dim > 0 && comment != "" {
		comment = dimClaimPattern.ReplaceAllString(comment, fmt.Sprintf("%d-dim", dim))
	}

	rebuilt := strings.Repeat(" ", indent) + key + ": " + model
	if comment != "" {
		rebuilt += "  " + comment
	}
	if strings.HasSuffix(lines[idx], "\n") {
		rebuilt += "\n"
	}
	lines[idx] = rebuilt

	// Atomic replace so a crash mid-write cannot leave a truncated config.
	// The path is filepath.Join(ggDir, ConfigFile) with a constant filename and a
	// ggDir resolved internally by config.GGDir(); no component comes from user
	// input, so the taint analyzer's path-traversal warning does not apply here.
	tmp := filepath.Clean(path + ".tmp")
	if err := os.WriteFile(tmp, []byte(strings.Join(lines, "")), 0o644); err != nil { //nolint:gosec // G703: internally-derived path, not user input
		return false, fmt.Errorf("write temp config: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return false, fmt.Errorf("replace config: %w", err)
	}
	return true, nil
}

// embeddingModelLine locates the `model:` line that is a direct child of the
// top-level `embedding:` key, returning its index and indentation, or -1.
func embeddingModelLine(lines []string) (int, int) {
	start := -1
	for i, l := range lines {
		if strings.TrimRight(l, "\r\n") == "embedding:" {
			start = i
			break
		}
	}
	if start < 0 {
		return -1, 0
	}
	childIndent := -1
	for i := start + 1; i < len(lines); i++ {
		line := strings.TrimRight(lines[i], "\r\n")
		if strings.TrimSpace(line) == "" {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		if indent == 0 {
			break // left the embedding block
		}
		if childIndent < 0 {
			childIndent = indent
		}
		if indent > childIndent {
			continue // nested (voyage:, etc.) — not our key
		}
		if strings.HasPrefix(strings.TrimSpace(line), "model:") {
			return i, indent
		}
	}
	return -1, 0
}

// splitYAMLLine breaks "  model: name  # note" into key, value and comment.
// The comment retains its leading '#'.
func splitYAMLLine(line string) (key, value, comment string) {
	body := strings.TrimRight(line, "\r\n")
	if h := strings.Index(body, "#"); h >= 0 {
		comment = strings.TrimSpace(body[h:])
		body = body[:h]
	}
	key, value, _ = strings.Cut(strings.TrimSpace(body), ":")
	return strings.TrimSpace(key), strings.TrimSpace(value), comment
}
