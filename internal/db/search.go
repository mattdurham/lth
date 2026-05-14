// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package db

import (
	"context"
	"fmt"
	"strings"
)

// VectorSearch performs a KNN search using the vec0 virtual table.
// It returns at most limit results from the given layers, ordered by ascending L2 distance.
// Only active (non-compacted) memories are returned.
func (d *DB) VectorSearch(ctx context.Context, emb []float32, layers []int, limit int) ([]*VectorResult, error) {
	if len(emb) == 0 {
		return nil, nil
	}

	embJSON, err := float32SliceToJSON(emb)
	if err != nil {
		return nil, fmt.Errorf("encode query embedding: %w", err)
	}

	layerFilter := buildLayerFilter(layers)

	//nolint:gosec // layer values are integer constants, not user input
	query := fmt.Sprintf(`
SELECT m.id, m.layer, m.content, m.content_hash, m.embedding, m.importance, m.access_count,
       m.created_at, m.updated_at, m.last_accessed_at, m.decay_rate, m.stability,
       m.source, m.agent, m.compacted_at, mv.distance
FROM memories_vec mv
JOIN memories m ON m.rowid = mv.rowid
WHERE mv.embedding MATCH ? AND k = ?
  AND m.compacted_at IS NULL
  %s
ORDER BY mv.distance ASC`, layerFilter)

	rows, err := d.db.QueryContext(ctx, query, embJSON, limit)
	if err != nil {
		return nil, fmt.Errorf("vector search: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var results []*VectorResult
	for rows.Next() {
		m := &MemoryRow{}
		var vr VectorResult
		var compactedAt interface{}
		var embBlob []byte

		err := rows.Scan(
			&m.ID, &m.Layer, &m.Content, &m.ContentHash, &embBlob, &m.Importance, &m.AccessCount,
			&m.CreatedAt, &m.UpdatedAt, &m.LastAccessedAt, &m.DecayRate, &m.Stability,
			&m.Source, &m.Agent, &compactedAt, &vr.Distance,
		)
		if err != nil {
			return nil, fmt.Errorf("scan vector result: %w", err)
		}
		m.Embedding = embBlob
		vr.MemoryRow = m
		results = append(results, &vr)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("vector search rows: %w", err)
	}

	return results, nil
}

// FTSSearch performs a full-text search using the FTS5 virtual table.
// The query is split on whitespace and each term is OR-joined for broad matching.
func (d *DB) FTSSearch(ctx context.Context, query string, layers []int, limit int) ([]*MemoryRow, error) {
	if query == "" {
		return nil, nil
	}

	ftsQuery := buildFTSQuery(query)
	layerFilter := buildLayerFilter(layers)

	//nolint:gosec // layer values are integer constants, not user input
	sqlQuery := fmt.Sprintf(`
SELECT m.id, m.layer, m.content, m.content_hash, m.embedding, m.importance, m.access_count,
       m.created_at, m.updated_at, m.last_accessed_at, m.decay_rate, m.stability,
       m.source, m.agent, m.compacted_at
FROM memories m
JOIN memories_fts fts ON fts.rowid = m.rowid
WHERE fts.content MATCH ? AND m.compacted_at IS NULL
  %s
ORDER BY bm25(memories_fts) ASC
LIMIT ?`, layerFilter)

	rows, err := d.db.QueryContext(ctx, sqlQuery, ftsQuery, limit)
	if err != nil {
		return nil, fmt.Errorf("fts search: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	return scanMemoryRows(rows)
}

// buildFTSQuery converts a natural language query into an FTS5 OR query.
// Each whitespace-separated term is quoted and joined with OR.
func buildFTSQuery(query string) string {
	terms := strings.Fields(query)
	if len(terms) == 0 {
		return ""
	}
	quoted := make([]string, len(terms))
	for i, t := range terms {
		// Quote the term to treat it as a literal phrase.
		t = strings.ReplaceAll(t, `"`, `""`)
		quoted[i] = `"` + t + `"`
	}
	return strings.Join(quoted, " OR ")
}

// buildLayerFilter builds a SQL fragment filtering by layer.
// Returns empty string if layers is nil/empty (all layers).
func buildLayerFilter(layers []int) string {
	if len(layers) == 0 {
		return ""
	}
	parts := make([]string, len(layers))
	for i, l := range layers {
		parts[i] = fmt.Sprintf("%d", l)
	}
	return fmt.Sprintf("AND m.layer IN (%s)", strings.Join(parts, ","))
}

// float32SliceToJSON encodes a []float32 as a JSON array string for vec0 MATCH queries.
func float32SliceToJSON(v []float32) (string, error) {
	if len(v) == 0 {
		return "[]", nil
	}
	var b strings.Builder
	b.WriteByte('[')
	for i, f := range v {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, "%g", f)
	}
	b.WriteByte(']')
	return b.String(), nil
}
