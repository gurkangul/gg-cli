package cmd

import (
	"context"
	"strings"
	"time"

	"github.com/gurkangul/gg/internal/config"
	"github.com/gurkangul/gg/internal/embedding"
	"github.com/gurkangul/gg/internal/store"
)

const cmdTimeout = 10 * time.Second

func cmdContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), cmdTimeout)
}

type deps struct {
	store   *store.Client
	embedder *embedding.Generator
}

func loadDeps(needEmbedding bool) (*deps, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	client, err := store.New(&cfg.Qdrant)
	if err != nil {
		return nil, err
	}
	d := &deps{store: client}
	if needEmbedding {
		d.embedder = embedding.New(&cfg.Embedding)
	}
	return d, nil
}

func (d *deps) Close() {
	if d.store != nil {
		d.store.Close()
	}
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
