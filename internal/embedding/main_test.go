package embedding

import (
	"os"
	"testing"

	"github.com/gurkangul/gg-cli/internal/config"
)

// TestMain neutralizes any ambient GG_EMBED_MODEL before the suite runs. Tests in
// this package assert behavior of the CONFIGURED model (e.g. "nomic-embed-text"),
// so a developer running the documented `export GG_EMBED_MODEL=qwen3-embedding:0.6b`
// workflow would otherwise see EffectiveModel/EffectiveModelIdentity/newOllamaBackend
// return the override and fail. Override-specific cases opt back in via t.Setenv,
// which restores the (now-unset) value afterward, keeping each test hermetic.
func TestMain(m *testing.M) {
	_ = os.Unsetenv(config.EmbedModelEnv)
	os.Exit(m.Run())
}
