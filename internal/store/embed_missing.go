package store

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/qdrant/go-client/qdrant"
)

// EmbedMissingResult summarises one collection's embed-missing pass.
type EmbedMissingResult struct {
	Collection string
	Embedded   int
	Skipped    int // points with no embeddable text
	AlreadyHad int // points that already had a vector
}

// EmbedMissing scans all project collections and re-embeds any point that
// has no vector (i.e. was imported without vectors via 'gg brain import').
//
// Points that already have a vector are left untouched — the operation is
// additive and idempotent. If the process is killed mid-run, re-running
// continues from where it left off because upsert by ID is idempotent.
//
// Progress is written to progressWriter line-by-line. Pass nil to suppress.
func (c *Client) EmbedMissing(ctx context.Context, embedder Embedder, progressWriter io.Writer) ([]EmbedMissingResult, error) {
	var results []EmbedMissingResult

	for _, suffix := range collectionSuffixes {
		coll := c.projectID + "-" + suffix
		extractor, ok := collTextExtractors[suffix]
		if !ok {
			continue
		}

		res, err := c.embedMissingInCollection(ctx, coll, suffix, embedder, extractor, progressWriter)
		if err != nil {
			return results, err
		}
		results = append(results, res)
	}
	return results, nil
}

func (c *Client) embedMissingInCollection(
	ctx context.Context,
	coll, suffix string,
	embedder Embedder,
	extractor func(map[string]*qdrant.Value) string,
	progressWriter io.Writer,
) (EmbedMissingResult, error) {
	res := EmbedMissingResult{Collection: coll}

	pageSize := uint32(64)
	withVecs := qdrant.NewWithVectorsEnable(true)
	withPay := qdrant.NewWithPayloadEnable(true)

	var offset *qdrant.PointId
	for {
		page, next, err := c.scroller.ScrollAndOffset(ctx, &qdrant.ScrollPoints{
			CollectionName: coll,
			Limit:          &pageSize,
			Offset:         offset,
			WithPayload:    withPay,
			WithVectors:    withVecs,
		})
		if err != nil {
			if isCollectionNotFoundError(err) {
				// Collection missing on fresh install — legitimate empty.
				return res, nil
			}
			fmt.Fprintf(os.Stderr, "gg: embed-missing: transient scroll error for %q: %v\n", coll, err)
			return res, fmt.Errorf("scroll %s: %w", coll, err)
		}

		for _, p := range page {
			id := p.GetId().GetUuid()
			if id == "" {
				continue
			}

			// Check if this point already has a vector.
			vec := extractVector(p.GetVectors())
			if len(vec) > 0 {
				res.AlreadyHad++
				continue
			}

			// Extract embeddable text from payload.
			text := extractor(p.GetPayload())
			if text == "" {
				res.Skipped++
				continue
			}

			// Generate embedding.
			generated, embErr := embedder.Generate(ctx, text)
			if embErr != nil {
				return res, fmt.Errorf("embed %s/%s: %w", suffix, id, embErr)
			}

			// Upsert with vector (payload stays as-is, only vector is added).
			wait := true
			if upsertErr := c.qdrantUpsert(ctx, &qdrant.UpsertPoints{
				CollectionName: coll,
				Wait:           &wait,
				Points: []*qdrant.PointStruct{
					{
						Id:      qdrant.NewID(id),
						Vectors: qdrant.NewVectors(generated...),
						Payload: p.GetPayload(),
					},
				},
			}); upsertErr != nil {
				return res, fmt.Errorf("upsert vector %s/%s: %w", suffix, id, upsertErr)
			}

			res.Embedded++
			if progressWriter != nil {
				fmt.Fprintf(progressWriter, "  embedding %s/%s\n", suffix, id[:8])
			}
		}

		if next == nil {
			break
		}
		offset = next
	}
	return res, nil
}
