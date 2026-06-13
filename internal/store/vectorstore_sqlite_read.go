package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"

	"github.com/qdrant/go-client/qdrant"
)

// candidate is one loaded point row during a scan.
type candidate struct {
	id      string
	vector  []float32
	payload map[string]*qdrant.Value
}

// loadCollection streams every point in a collection, decoding vectors only
// when withVectors is true (Query always needs them; Scroll needs them only
// when WithVectors was requested; Count never does). A missing collection
// returns the NotFound-equivalent error so callers' isCollectionNotFoundError
// checks keep working.
func (s *sqliteStore) loadCollection(ctx context.Context, coll string, withVectors bool) ([]candidate, error) {
	exists, err := s.collectionExists(ctx, coll)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errCollectionNotFound(coll)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, vector, payload FROM points WHERE collection=?`, coll)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []candidate
	for rows.Next() {
		var id string
		var vecBlob, payBlob []byte
		if err := rows.Scan(&id, &vecBlob, &payBlob); err != nil {
			return nil, err
		}
		payload, err := decodePayload(payBlob)
		if err != nil {
			return nil, err
		}
		c := candidate{id: id, payload: payload}
		if withVectors {
			c.vector = decodeVector(vecBlob)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Query performs an exact brute-force cosine top-K search: it loads the
// filtered candidate set, scores each against the query vector, applies the
// score threshold, and returns the highest-scoring points (limit, default 10).
func (s *sqliteStore) Query(ctx context.Context, req *qdrant.QueryPoints) ([]*qdrant.ScoredPoint, error) {
	if req == nil {
		return nil, fmt.Errorf("query: nil request")
	}
	queryVec := req.GetQuery().GetNearest().GetDense().GetData()
	if len(queryVec) == 0 {
		return nil, fmt.Errorf("query %s: missing or non-dense query vector", req.CollectionName)
	}
	cands, err := s.loadCollection(ctx, req.CollectionName, true)
	if err != nil {
		return nil, err
	}
	filter := req.GetFilter()
	hasThreshold := req.ScoreThreshold != nil
	var threshold float32
	if hasThreshold {
		threshold = req.GetScoreThreshold()
	}

	scored := make([]*qdrant.ScoredPoint, 0, len(cands))
	for i := range cands {
		c := &cands[i]
		if !matchFilter(c.payload, filter) {
			continue
		}
		score := cosineSimilarity(queryVec, c.vector)
		if hasThreshold && score < threshold {
			continue
		}
		scored = append(scored, &qdrant.ScoredPoint{
			Id:      keyToPointID(c.id),
			Payload: projectPayload(c.payload, req.GetWithPayload()),
			Score:   score,
		})
	}
	// Highest score first; ties broken by id for deterministic ordering.
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].Score != scored[j].Score {
			return scored[i].Score > scored[j].Score
		}
		return scored[i].GetId().GetUuid() < scored[j].GetId().GetUuid()
	})

	limit := uint64(10)
	if req.Limit != nil {
		limit = req.GetLimit()
	}
	if uint64(len(scored)) > limit {
		scored = scored[:limit]
	}
	return scored, nil
}

// Get returns the points with the requested ids, with payload projected per the
// WithPayloadSelector. Order follows the request id order, and missing ids are
// silently skipped (Qdrant behavior).
func (s *sqliteStore) Get(ctx context.Context, req *qdrant.GetPoints) ([]*qdrant.RetrievedPoint, error) {
	if req == nil {
		return nil, fmt.Errorf("get: nil request")
	}
	exists, err := s.collectionExists(ctx, req.CollectionName)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errCollectionNotFound(req.CollectionName)
	}
	withVecs := wantsVectors(req.GetWithVectors())
	out := make([]*qdrant.RetrievedPoint, 0, len(req.GetIds()))
	for _, id := range req.GetIds() {
		key := pointIDKey(id)
		var payBlob, vecBlob []byte
		row := s.db.QueryRowContext(ctx, `SELECT payload, vector FROM points WHERE collection=? AND id=?`, req.CollectionName, key)
		if err := row.Scan(&payBlob, &vecBlob); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			return nil, err
		}
		payload, err := decodePayload(payBlob)
		if err != nil {
			return nil, err
		}
		rp := &qdrant.RetrievedPoint{
			Id:      keyToPointID(key),
			Payload: projectPayload(payload, req.GetWithPayload()),
		}
		if withVecs {
			rp.Vectors = outputVectors(decodeVector(vecBlob))
		}
		out = append(out, rp)
	}
	return out, nil
}

// ScrollAndOffset paginates points ordered by id, replicating Qdrant's cursor
// contract: Offset is the inclusive start id, the page holds at most Limit
// points, and the returned next offset is the id of the first point of the
// following page (nil when exhausted).
func (s *sqliteStore) ScrollAndOffset(ctx context.Context, req *qdrant.ScrollPoints) ([]*qdrant.RetrievedPoint, *qdrant.PointId, error) {
	if req == nil {
		return nil, nil, fmt.Errorf("scroll: nil request")
	}
	withVecs := wantsVectors(req.GetWithVectors())
	cands, err := s.loadCollection(ctx, req.CollectionName, withVecs)
	if err != nil {
		return nil, nil, err
	}
	filter := req.GetFilter()
	matched := make([]candidate, 0, len(cands))
	for _, c := range cands {
		if matchFilter(c.payload, filter) {
			matched = append(matched, c)
		}
	}
	sort.Slice(matched, func(i, j int) bool { return matched[i].id < matched[j].id })

	start := 0
	if req.Offset != nil {
		startKey := pointIDKey(req.Offset)
		start = sort.Search(len(matched), func(i int) bool { return matched[i].id >= startKey })
	}
	limit := len(matched) - start
	if req.Limit != nil && int(*req.Limit) < limit {
		limit = int(*req.Limit)
	}
	end := start + limit
	if end > len(matched) {
		end = len(matched)
	}

	page := make([]*qdrant.RetrievedPoint, 0, end-start)
	for i := start; i < end; i++ {
		rp := &qdrant.RetrievedPoint{
			Id:      keyToPointID(matched[i].id),
			Payload: projectPayload(matched[i].payload, req.GetWithPayload()),
		}
		if withVecs {
			rp.Vectors = outputVectors(matched[i].vector)
		}
		page = append(page, rp)
	}
	var next *qdrant.PointId
	if end < len(matched) {
		next = keyToPointID(matched[end].id)
	}
	return page, next, nil
}

// Count returns the number of points in a collection matching the optional
// filter. A missing collection surfaces NotFound so callers can treat it as an
// empty/fresh install.
func (s *sqliteStore) Count(ctx context.Context, req *qdrant.CountPoints) (uint64, error) {
	if req == nil {
		return 0, fmt.Errorf("count: nil request")
	}
	if req.GetFilter() == nil {
		exists, err := s.collectionExists(ctx, req.CollectionName)
		if err != nil {
			return 0, err
		}
		if !exists {
			return 0, errCollectionNotFound(req.CollectionName)
		}
		var n uint64
		row := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM points WHERE collection=?`, req.CollectionName)
		if err := row.Scan(&n); err != nil {
			return 0, err
		}
		return n, nil
	}
	cands, err := s.loadCollection(ctx, req.CollectionName, false)
	if err != nil {
		return 0, err
	}
	var n uint64
	for _, c := range cands {
		if matchFilter(c.payload, req.GetFilter()) {
			n++
		}
	}
	return n, nil
}
