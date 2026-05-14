// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package memory

import (
	"context"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/mattdurham/lth/internal/db"
	"github.com/mattdurham/lth/internal/vector"
)

const scoringLambda = 0.995 // decay constant per hour

// Search performs multi-modal search and returns at most TopK scored results.
func (s *MemoryStore) Search(ctx context.Context, req *SearchRequest) ([]*ScoredMemory, error) {
	s.applySearchDefaults(req)

	// Embed the query for vector search and scoring.
	var queryEmb []float32
	var err error
	if s.emb != nil && req.Query != "" {
		queryEmb, err = s.emb.Embed(ctx, req.Query)
		if err != nil {
			queryEmb = nil // degrade to FTS5-only
		}
	}

	// Run vector and FTS searches in parallel.
	vecResults, ftsResults := s.runParallelSearches(ctx, req, queryEmb)

	// Merge results by ID.
	candidates := mergeCandidates(vecResults, ftsResults)

	now := time.Now().UTC()
	scored := make([]*ScoredMemory, 0, len(candidates))
	for _, row := range candidates {
		sc := scoreMemory(row, queryEmb, now, req.Alpha, req.Beta, req.Gamma)
		attrs, err := s.db.GetAttributes(ctx, row.ID)
		if err != nil {
			attrs = nil // degrade gracefully; search result is still returned
		}
		m := rowToMemory(row, attrs)
		scored = append(scored, &ScoredMemory{
			Memory:          m,
			Score:           sc.total,
			TimeScore:       sc.timeScore,
			ImportanceScore: sc.importScore,
			VectorScore:     sc.cosScore,
		})
	}

	// Sort by score descending.
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].Score > scored[j].Score
	})

	// Limit to TopK.
	if len(scored) > req.TopK {
		scored = scored[:req.TopK]
	}

	// Mark accessed for all returned results.
	now2 := time.Now().UTC()
	for _, sm := range scored {
		_ = s.db.MarkAccessed(ctx, sm.ID, now2)
	}

	return scored, nil
}

type scoreBreakdown struct {
	total       float32
	timeScore   float32
	importScore float32
	cosScore    float32
}

// scoreMemory computes the composite score for a single memory row.
func scoreMemory(row *db.MemoryRow, queryEmb []float32, now time.Time, alpha, beta, gamma float32) scoreBreakdown {
	hours := now.Sub(row.CreatedAt).Hours()
	timeScore := alpha * float32(math.Exp(-scoringLambda*hours))

	importScore := beta * row.Importance / 10.0

	var cosScore float32
	if len(queryEmb) > 0 && len(row.Embedding) > 0 {
		memEmb := vector.FromBytes(row.Embedding)
		cosScore = gamma * vector.Cosine(queryEmb, memEmb)
	}

	return scoreBreakdown{
		total:       timeScore + importScore + cosScore,
		timeScore:   timeScore,
		importScore: importScore,
		cosScore:    cosScore,
	}
}

// runParallelSearches runs VectorSearch and FTSSearch concurrently and returns their results.
func (s *MemoryStore) runParallelSearches(ctx context.Context, req *SearchRequest, queryEmb []float32) ([]*db.VectorResult, []*db.MemoryRow) {
	var (
		vecResults []*db.VectorResult
		ftsResults []*db.MemoryRow
		wg         sync.WaitGroup
	)

	k := req.TopK * 3 // over-fetch to improve ranking quality

	wg.Add(1)
	go func() {
		defer wg.Done()
		if len(queryEmb) > 0 {
			r, err := s.db.VectorSearch(ctx, queryEmb, req.Layers, k)
			if err == nil {
				vecResults = r
			}
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		if req.Query != "" {
			r, err := s.db.FTSSearch(ctx, req.Query, req.Layers, k)
			if err == nil {
				ftsResults = r
			}
		}
	}()

	wg.Wait()
	return vecResults, ftsResults
}

// mergeCandidates deduplicates vector and FTS results by memory ID.
func mergeCandidates(vecResults []*db.VectorResult, ftsResults []*db.MemoryRow) []*db.MemoryRow {
	seen := make(map[string]bool)
	var result []*db.MemoryRow

	for _, vr := range vecResults {
		if !seen[vr.ID] {
			seen[vr.ID] = true
			result = append(result, vr.MemoryRow)
		}
	}
	for _, m := range ftsResults {
		if !seen[m.ID] {
			seen[m.ID] = true
			result = append(result, m)
		}
	}
	return result
}

// applySearchDefaults fills in zero-value fields in a SearchRequest with config defaults.
func (s *MemoryStore) applySearchDefaults(req *SearchRequest) {
	if req.TopK == 0 {
		req.TopK = s.cfg.Search.DefaultTopK
	}
	if req.Alpha == 0 && req.Beta == 0 && req.Gamma == 0 {
		req.Alpha = s.cfg.Search.Alpha
		req.Beta = s.cfg.Search.Beta
		req.Gamma = s.cfg.Search.Gamma
	}
	if len(req.Layers) == 0 {
		req.Layers = []int{1, 2, 3, 4, 5}
	}
}
