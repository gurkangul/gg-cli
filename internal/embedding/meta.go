package embedding

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const metaFile = "embedding-meta.json"

// ErrModelMismatch is returned when the configured embedding model differs from
// the model recorded in the stored metadata. The caller should direct the user
// to run `gg reembed` to migrate existing vectors.
var ErrModelMismatch = errors.New("embedding model mismatch")

// Meta records which embedding model and vector dimension were used when the
// Qdrant collections for this project were created. If this doesn't match the
// currently configured model, vectors from different models would be mixed in
// the same collection — breaking semantic search recall.
type Meta struct {
	ModelName string `json:"model_name"`
	Dim       int    `json:"dim"`
	CreatedAt string `json:"created_at"`
}

// ReadMeta reads the embedding metadata from <ggDir>/embedding-meta.json.
// Returns (nil, nil) when the file does not exist (first run).
func ReadMeta(ggDir string) (*Meta, error) {
	data, err := os.ReadFile(metaPath(ggDir))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read embedding meta: %w", err)
	}
	var m Meta
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse embedding meta: %w", err)
	}
	return &m, nil
}

// WriteMeta persists embedding metadata. Overwrites any existing file.
func WriteMeta(ggDir string, m *Meta) error {
	if m.CreatedAt == "" {
		m.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	data, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("marshal embedding meta: %w", err)
	}
	if err := os.MkdirAll(ggDir, 0o755); err != nil {
		return fmt.Errorf("mkdir for embedding meta: %w", err)
	}
	return os.WriteFile(metaPath(ggDir), data, 0o644)
}

// CheckMeta compares the current model name against stored metadata.
//
//   - If no metadata file exists (first run), it writes a new one and returns nil.
//   - If metadata exists and the model matches, returns nil.
//   - If metadata exists and the model differs, returns ErrModelMismatch (wrapped),
//     describing both the stored and the configured model.
func CheckMeta(ggDir, modelName string, dim int) error {
	meta, err := ReadMeta(ggDir)
	if err != nil {
		return err
	}
	if meta == nil {
		// First run — record the current model as authoritative.
		return WriteMeta(ggDir, &Meta{ModelName: modelName, Dim: dim})
	}
	if meta.ModelName != modelName {
		return fmt.Errorf(
			"%w: Qdrant collections were created with model %q (dim %d), "+
				"but the project now uses %q — "+
				"run `gg reembed` to drop and rebuild all collections with the new model",
			ErrModelMismatch, meta.ModelName, meta.Dim, modelName,
		)
	}
	return nil
}

func metaPath(ggDir string) string {
	return filepath.Join(ggDir, metaFile)
}
