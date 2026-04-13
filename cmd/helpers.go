package cmd

import (
	"fmt"
	"strings"

	"github.com/gurkangul/gg/internal/config"
	"github.com/gurkangul/gg/internal/embedding"
	"github.com/gurkangul/gg/internal/store"
)

func newStoreClient() (*store.Client, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	return store.New(&cfg.Qdrant)
}

func newEmbedder() (*embedding.Generator, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	if cfg.Embedding.APIKey == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY not set — export it or set in .gg/config.yaml")
	}
	return embedding.New(&cfg.Embedding), nil
}

func parseTags(tags string) []string {
	if tags == "" {
		return nil
	}
	parts := strings.Split(tags, ",")
	var result []string
	for _, p := range parts {
		t := strings.TrimSpace(p)
		if t != "" {
			result = append(result, t)
		}
	}
	return result
}
