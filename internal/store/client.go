package store

import (
	"context"
	"fmt"

	"github.com/gurkangul/gg/internal/config"
	"github.com/qdrant/go-client/qdrant"
)

const (
	collSuffixDecisions   = "decisions"
	collSuffixTasks       = "tasks"
	collSuffixMessages    = "messages"
	collSuffixRejections  = "rejections"
	collSuffixDiscussions = "discussions"

	VectorSize = 768 // nomic-embed-text dimension
)

type Client struct {
	qc        *qdrant.Client
	dataDir   string // absolute path to project-local .gg/ — used for file-locked counters
	projectID string // per-project namespace prefix for Qdrant collections
}

// New creates a store client bound to a specific project. All Qdrant
// collection names are prefixed with projectID so multiple projects can share
// a single Qdrant instance without seeing each other's data.
func New(cfg *config.QdrantConfig, dataDir, projectID string) (*Client, error) {
	if projectID == "" {
		return nil, fmt.Errorf("project ID is required — config missing project_id")
	}
	qc, err := qdrant.NewClient(&qdrant.Config{
		Host:                   cfg.Host,
		Port:                   cfg.Port,
		SkipCompatibilityCheck: true,
	})
	if err != nil {
		return nil, fmt.Errorf("qdrant connect failed: %w", err)
	}
	return &Client{qc: qc, dataDir: dataDir, projectID: projectID}, nil
}

func (c *Client) Close() error {
	return c.qc.Close()
}

func (c *Client) HealthCheck(ctx context.Context) error {
	_, err := c.qc.HealthCheck(ctx)
	return err
}

// Collection names are composed of <project_id>-<type>. The project_id is a
// UUID, safe for collection names.
func (c *Client) collDecisions() string   { return c.projectID + "-" + collSuffixDecisions }
func (c *Client) collTasks() string       { return c.projectID + "-" + collSuffixTasks }
func (c *Client) collMessages() string    { return c.projectID + "-" + collSuffixMessages }
func (c *Client) collRejections() string  { return c.projectID + "-" + collSuffixRejections }
func (c *Client) collDiscussions() string { return c.projectID + "-" + collSuffixDiscussions }

// EnsureCollections creates this project's Qdrant collections if they don't exist.
func (c *Client) EnsureCollections(ctx context.Context) error {
	collections := []string{c.collDecisions(), c.collTasks(), c.collMessages(), c.collRejections(), c.collDiscussions()}
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
