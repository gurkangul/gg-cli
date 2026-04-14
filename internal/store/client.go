package store

import (
	"context"
	"fmt"

	"github.com/gurkangul/gg/internal/config"
	"github.com/qdrant/go-client/qdrant"
)

const (
	CollDecisions  = "decisions"
	CollTasks      = "tasks"
	CollMessages   = "messages"
	CollRejections = "rejections"

	VectorSize = 768 // nomic-embed-text dimension
)

type Client struct {
	qc      *qdrant.Client
	dataDir string // absolute path to .gg/ — used for file-locked counters
}

func New(cfg *config.QdrantConfig, dataDir string) (*Client, error) {
	qc, err := qdrant.NewClient(&qdrant.Config{
		Host: cfg.Host,
		Port: cfg.Port,
	})
	if err != nil {
		return nil, fmt.Errorf("qdrant connect failed: %w", err)
	}
	return &Client{qc: qc, dataDir: dataDir}, nil
}

func (c *Client) Close() error {
	return c.qc.Close()
}

func (c *Client) HealthCheck(ctx context.Context) error {
	_, err := c.qc.HealthCheck(ctx)
	return err
}

// EnsureCollections creates all required Qdrant collections if they don't exist.
func (c *Client) EnsureCollections(ctx context.Context) error {
	collections := []string{CollDecisions, CollTasks, CollMessages, CollRejections}
	existing, err := c.qc.ListCollections(ctx)
	if err != nil {
		return fmt.Errorf("list collections: %w", err)
	}
	existMap := make(map[string]bool)
	for _, name := range existing {
		existMap[name] = true
	}

	vectorCfg := qdrant.NewVectorsConfig(&qdrant.VectorParams{
		Size:     VectorSize,
		Distance: qdrant.Distance_Cosine,
	})
	for _, name := range collections {
		if existMap[name] {
			continue
		}
		err := c.qc.CreateCollection(ctx, &qdrant.CreateCollection{
			CollectionName: name,
			VectorsConfig:  vectorCfg,
		})
		if err != nil {
			return fmt.Errorf("create collection %s: %w", name, err)
		}
	}
	return nil
}
