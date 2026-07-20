package store

import (
	"context"
	"fmt"
	"strings"
)

// heal.go — TASK-521 bounded, non-interactive repair of degraded vectors.
//
// When the embedder is unreachable at write time, offline reconcile upserts a
// placeholder zero-vector stamped gg_vector_degraded. Those points stay out of
// semantic search on purpose (BUG-066: a zero vector ranks by garbage cosine),
// and since TASK-516 a coverage notice tells the reader about them. But nothing
// ever repaired them: the remedy printed was "run gg reembed", which assumes a
// human is present. For a headless agent there is no such human, so a project
// could sit permanently degraded — visible, explained, and never fixed.
//
// This closes that loop. It is deliberately NOT a full reembed: reembed drops
// and rebuilds every collection, which is far too heavy to trigger from a read.
// Healing only touches the points that are actually broken, one embed call each.
//
// Every stopping condition returns what was healed so far rather than an error:
// a read must never fail because an opportunistic repair could not finish.

// HealDegradedVectors re-embeds up to max points that carry a placeholder
// zero-vector, clearing their degraded markers so semantic search can see them
// again. Returns how many were healed.
//
// Stops early — without error — when max is reached, ctx expires, or the
// embedder becomes unavailable. Whatever is left stays marked and is picked up
// by the next call, so partial progress is always kept.
//
// Messages are naturally excluded: their zero vector is intentional (they are
// role-filtered inbox items, not semantic documents) and they are never marked
// degraded in the first place.
func (c *Client) HealDegradedVectors(ctx context.Context, embedder Embedder, max int) (healed int, err error) {
	if c == nil || embedder == nil || max <= 0 {
		return 0, nil
	}

	for suffix, collName := range semanticCollectionNames(c) {
		extractor, ok := collTextExtractors[suffix]
		if !ok {
			continue
		}
		points, scanErr := c.scrollAll(ctx, &ScrollPoints{
			CollectionName: collName,
			Limit:          PtrOf(uint32(1000)),
			WithPayload:    NewWithPayloadEnable(true),
		})
		if scanErr != nil {
			if isCollectionNotFoundError(scanErr) {
				continue // nothing indexed for this kind yet
			}
			return healed, fmt.Errorf("scan degraded vectors in %s: %w", suffix, scanErr)
		}

		for _, p := range points {
			if healed >= max || ctx.Err() != nil {
				return healed, nil
			}
			payload := p.GetPayload()
			if payload["gg_vector_degraded"].GetStringValue() == "" {
				continue
			}
			text := strings.TrimSpace(extractor(payload))
			if text == "" {
				// Nothing to embed. Leave the marker in place so the record stays
				// reported as degraded rather than silently counted as repaired.
				continue
			}
			vec, embedErr := embedder.Generate(ctx, text)
			if embedErr != nil || len(vec) == 0 {
				// Embedder went away mid-pass. Stop cleanly and keep the progress
				// already made; the next read tries again.
				return healed, nil
			}
			if upsertErr := c.UpsertWithVector(ctx, suffix, p.GetId().GetUuid(), vec, clearDegradedVectorMarkers(payload), false); upsertErr != nil {
				return healed, fmt.Errorf("upsert healed vector in %s: %w", suffix, upsertErr)
			}
			healed++
		}
	}
	return healed, nil
}
