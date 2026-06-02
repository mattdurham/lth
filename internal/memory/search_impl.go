// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mattdurham/lth/internal/db"
	"github.com/mattdurham/lth/internal/vector"
)

// delta is the weight for the valence contribution in the composite score.
// The non-linear transform (valence × |valence|) suppresses near-zero values naturally.
const delta = float32(0.15)

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

	// Expand query via LLM if requested — generates additional related queries
	// whose candidates are merged before scoring.
	candidateQueries := []string{req.Query}
	if req.Expand && req.Query != "" {
		candidateQueries = s.expandQuery(ctx, req.Query, 3)
	}

	var allVec []*db.VectorResult
	var allFTS []*db.MemoryRow
	for i, q := range candidateQueries {
		var emb []float32
		if i == 0 {
			emb = queryEmb // already computed
		} else if s.emb != nil {
			emb, _ = s.emb.Embed(ctx, q)
		}
		origQuery := req.Query
		req.Query = q
		vecs, fts := s.runParallelSearches(ctx, req, emb)
		req.Query = origQuery
		allVec = append(allVec, vecs...)
		allFTS = append(allFTS, fts...)
	}

	// Merge results by ID.
	candidates := mergeCandidates(allVec, allFTS)

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
			ValenceScore:    sc.valenceScore,
		})
	}

	// Sort by score descending.
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].Score > scored[j].Score
	})

	// Boost memories whose attributes match all FilterAttrs key=value pairs.
	if len(req.FilterAttrs) > 0 {
		for _, sm := range scored {
			if attrsMatch(sm.Attrs, req.FilterAttrs) {
				sm.Score *= 1.5
			}
		}
		// Re-sort after boost.
		sort.Slice(scored, func(i, j int) bool {
			return scored[i].Score > scored[j].Score
		})
	}

	// Post-filter by valence range if specified.
	if req.MinValence != nil || req.MaxValence != nil {
		filtered := scored[:0]
		for _, sm := range scored {
			if req.MinValence != nil && sm.Valence < *req.MinValence {
				continue
			}
			if req.MaxValence != nil && sm.Valence > *req.MaxValence {
				continue
			}
			filtered = append(filtered, sm)
		}
		scored = filtered
	}

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

// scoreMemory computes the composite score for a single memory row.
// The composite formula is: α·exp(-λ·Δt) + β·importance/10 + γ·cosine(q,m) + δ·(v×|v|)
// where v×|v| is a sign-preserving square that amplifies extremes and suppresses near-zero noise.
func scoreMemory(row *db.MemoryRow, queryEmb []float32, now time.Time, alpha, beta, gamma float32) scoreBreakdown {
	hours := now.Sub(row.CreatedAt).Hours()
	timeScore := alpha * float32(math.Exp(-scoringLambda*hours))

	importScore := beta * row.Importance / 10.0

	var cosScore float32
	if len(queryEmb) > 0 && len(row.Embedding) > 0 {
		memEmb := vector.FromBytes(row.Embedding)
		cosScore = gamma * vector.Cosine(queryEmb, memEmb)
	}

	// Non-linear valence contribution: valence × |valence|
	// +1.0→+1.0, +0.5→+0.25, 0.0→0.0, -0.5→-0.25, -1.0→-1.0
	// Amplifies extremes, suppresses near-zero noise.
	valenceContrib := delta * float32(float64(row.Valence)*math.Abs(float64(row.Valence)))

	return scoreBreakdown{
		total:        timeScore + importScore + cosScore + valenceContrib,
		timeScore:    timeScore,
		importScore:  importScore,
		cosScore:     cosScore,
		valenceScore: valenceContrib,
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

// expandQuery uses the LLM to generate n alternative search queries related to the input.
// Returns the original query as the first element plus any expansions. Falls back to
// []string{query} on any error so it never blocks Search.
func (s *MemoryStore) expandQuery(ctx context.Context, query string, n int) []string {
	if s.llm == nil {
		return []string{query}
	}
	prompt := fmt.Sprintf(
		"Search query: %q\n\nGenerate %d alternative search queries that would find related but differently-worded technical content in a code/engineering memory store.\nReturn ONLY a JSON array of strings. No explanation. Example: [\"query one\",\"query two\"]",
		query, n,
	)
	resp, err := s.llm.Complete(ctx, prompt)
	if err != nil {
		return []string{query}
	}
	resp = strings.TrimSpace(resp)
	resp = strings.TrimPrefix(resp, "```json")
	resp = strings.TrimPrefix(resp, "```")
	resp = strings.TrimSuffix(resp, "```")
	resp = strings.TrimSpace(resp)

	var extras []string
	if err := json.Unmarshal([]byte(resp), &extras); err != nil {
		return []string{query}
	}
	result := []string{query}
	for _, e := range extras {
		if e = strings.TrimSpace(e); e != "" && e != query {
			result = append(result, e)
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

// attrsMatch returns true if memory attrs contain all key=value pairs in filter.
func attrsMatch(attrs, filter map[string]string) bool {
	for k, v := range filter {
		if attrs[k] != v {
			return false
		}
	}
	return true
}
