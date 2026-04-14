package store

import (
	"context"
	"fmt"

	"github.com/qdrant/go-client/qdrant"
)

// Embedder is the minimal interface required by ReembedAll.
type Embedder interface {
	Generate(ctx context.Context, text string) ([]float32, error)
}

// collTextExtractor maps a collection name suffix to a function that extracts
// embeddable text from a Qdrant payload. This mirrors the original embed logic
// used when each entity was first stored.
var collTextExtractors = map[string]func(map[string]*qdrant.Value) string{
	collSuffixDecisions: func(p map[string]*qdrant.Value) string {
		text := p["text"].GetStringValue()
		reason := p["reason"].GetStringValue()
		if reason != "" {
			return text + " " + reason
		}
		return text
	},
	collSuffixRejections: func(p map[string]*qdrant.Value) string {
		// AddRejection stores text under "approach", not "text".
		approach := p["approach"].GetStringValue()
		reason := p["reason"].GetStringValue()
		if reason != "" {
			return approach + " " + reason
		}
		return approach
	},
	collSuffixTasks: func(p map[string]*qdrant.Value) string {
		title := p["title"].GetStringValue()
		detail := p["detail"].GetStringValue()
		if detail != "" {
			return title + " " + detail
		}
		return title
	},
	collSuffixBugs: func(p map[string]*qdrant.Value) string {
		title := p["title"].GetStringValue()
		detail := p["detail"].GetStringValue()
		if detail != "" {
			return title + " " + detail
		}
		return title
	},
	collSuffixNotes: func(p map[string]*qdrant.Value) string {
		return p["text"].GetStringValue()
	},
	collSuffixMessages: func(p map[string]*qdrant.Value) string {
		// SendMessage stores body under "content", not "text".
		return p["content"].GetStringValue()
	},
	collSuffixDiscussions: func(p map[string]*qdrant.Value) string {
		// OpenDiscussion stores headline under "topic", not "question".
		topic := p["topic"].GetStringValue()
		detail := p["detail"].GetStringValue()
		if detail != "" {
			return topic + " " + detail
		}
		return topic
	},
}

// collectionSuffixes lists all project collection suffixes in a stable order.
var collectionSuffixes = []string{
	collSuffixDecisions,
	collSuffixTasks,
	collSuffixMessages,
	collSuffixRejections,
	collSuffixDiscussions,
	collSuffixNotes,
	collSuffixBugs,
}

// ReembedResult summarises the outcome of a ReembedAll run.
type ReembedResult struct {
	Collection string
	Migrated   int
	Skipped    int // points with no embeddable text
}

// ReembedAll migrates all project collections to a new embedding model/dimension.
//
// The migration steps are:
//  1. Scroll all points (with payload) from each collection.
//  2. Drop all project collections.
//  3. Recreate them at newVectorSize.
//  4. Re-embed each point using embedder and insert into the new collections.
//
// If step 4 fails partway, the caller must retry — the partially-migrated state
// will leave some collections empty. To restart cleanly, call DropAllCollections
// and EnsureCollections before retrying ReembedAll.
func (c *Client) ReembedAll(ctx context.Context, embedder Embedder, newVectorSize uint64) ([]ReembedResult, error) {
	// --- Step 1: Read all existing points before dropping collections ---
	type savedPoint struct {
		id      string
		payload map[string]*qdrant.Value
		text    string
	}
	type collectionData struct {
		name     string
		suffix   string
		points   []savedPoint
		extractor func(map[string]*qdrant.Value) string
	}

	collData := make([]collectionData, 0, len(collectionSuffixes))
	for _, suffix := range collectionSuffixes {
		name := c.projectID + "-" + suffix
		extractor, ok := collTextExtractors[suffix]
		if !ok {
			continue
		}
		points, err := c.scrollAll(ctx, &qdrant.ScrollPoints{
			CollectionName: name,
			WithPayload:    qdrant.NewWithPayloadEnable(true),
		})
		if err != nil {
			// Collection may not exist yet; treat as empty.
			collData = append(collData, collectionData{name: name, suffix: suffix, extractor: extractor})
			continue
		}
		saved := make([]savedPoint, 0, len(points))
		for _, p := range points {
			saved = append(saved, savedPoint{
				id:      p.GetId().GetUuid(),
				payload: p.GetPayload(),
				text:    extractor(p.GetPayload()),
			})
		}
		collData = append(collData, collectionData{
			name:      name,
			suffix:    suffix,
			points:    saved,
			extractor: extractor,
		})
	}

	// --- Step 2: Drop all project collections ---
	if err := c.DropAllCollections(ctx); err != nil {
		return nil, fmt.Errorf("reembed drop collections: %w", err)
	}

	// --- Step 3: Recreate with new vector size ---
	if err := c.EnsureCollections(ctx, newVectorSize); err != nil {
		return nil, fmt.Errorf("reembed create collections: %w", err)
	}

	// --- Step 4: Re-embed and upsert ---
	var results []ReembedResult
	for _, cd := range collData {
		res := ReembedResult{Collection: cd.name}
		for _, sp := range cd.points {
			if sp.text == "" {
				res.Skipped++
				continue
			}
			vec, err := embedder.Generate(ctx, sp.text)
			if err != nil {
				return results, fmt.Errorf("reembed %s point %s: generate: %w", cd.name, sp.id, err)
			}

			wait := true
			upsertErr := c.qdrantUpsert(ctx, &qdrant.UpsertPoints{
				CollectionName: cd.name,
				Wait:           &wait,
				Points: []*qdrant.PointStruct{
					{
						Id:      qdrant.NewID(sp.id),
						Vectors: qdrant.NewVectors(vec...),
						Payload: sp.payload,
					},
				},
			})
			if upsertErr != nil {
				return results, fmt.Errorf("reembed %s point %s: upsert: %w", cd.name, sp.id, upsertErr)
			}
			res.Migrated++
		}
		results = append(results, res)
	}
	return results, nil
}
