// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package db

import (
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"time"
)

// InsertMemory inserts a new memory row into memories and memories_vec.
// The embedding is stored as a BLOB in memories and as a JSON vector in memories_vec.
// Both inserts happen in the same transaction.
func (d *DB) InsertMemory(ctx context.Context, m *MemoryRow) error {
	if len(m.Embedding) > 0 {
		dim := len(m.Embedding) / 4
		if _, err := d.db.ExecContext(ctx, fmt.Sprintf(schemaVec, dim)); err != nil {
			return fmt.Errorf("ensure vec table: %w", err)
		}
	}
	return d.WithTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `
INSERT INTO memories
	(id, layer, content, content_hash, embedding, importance, access_count,
	 created_at, updated_at, last_accessed_at, decay_rate, stability, source, agent,
	 valence, valence_scored, embedding_model)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			m.ID, m.Layer, m.Content, m.ContentHash, m.Embedding, m.Importance, m.AccessCount,
			m.CreatedAt.UTC(), m.UpdatedAt.UTC(), m.LastAccessedAt.UTC(),
			m.DecayRate, m.Stability, m.Source, m.Agent,
			m.Valence, m.ValenceScored, m.EmbeddingModel,
		)
		if err != nil {
			return fmt.Errorf("insert memory: %w", err)
		}

		rowID, err := res.LastInsertId()
		if err != nil {
			return fmt.Errorf("last insert id: %w", err)
		}

		// Only insert into vec table if embedding is provided and dimension matches.
		// If dim mismatches (e.g. imported from another machine), skip vec insert —
		// BackfillEmbeddings will re-embed with the correct model.
		if len(m.Embedding) > 0 && (d.embedDim == 0 || len(m.Embedding)/4 == d.embedDim) {
			embJSON, err := embeddingBytesToJSON(m.Embedding)
			if err != nil {
				return fmt.Errorf("encode embedding for vec: %w", err)
			}
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO memories_vec(rowid, embedding) VALUES (?, ?)`,
				rowID, embJSON,
			); err != nil {
				return fmt.Errorf("insert vec: %w", err)
			}
		}

		return nil
	})
}

// InsertMemoryBatch upserts a slice of memories in a single transaction.
// New rows are inserted; existing rows (matched by content_hash) are updated
// only when the incoming updated_at is strictly newer than the stored value.
// attrs maps memory ID to its key/value attributes; may be nil.
// Returns counts of inserted, updated, and skipped (existing and not newer) rows.
func (d *DB) InsertMemoryBatch(ctx context.Context, rows []*MemoryRow, attrs map[string]map[string]string) (inserted, updated, skipped int, err error) {
	// Ensure memories_vec exists before opening the transaction — DDL cannot run
	// on d.db while the single connection is held by the transaction.
	for _, m := range rows {
		if len(m.Embedding) > 0 {
			dim := len(m.Embedding) / 4
			if _, err := d.db.ExecContext(ctx, fmt.Sprintf(schemaVec, dim)); err != nil {
				return 0, 0, 0, fmt.Errorf("ensure vec table: %w", err)
			}
			break
		}
	}

	// Build a content_hash → incoming attrs index so we can apply attrs after
	// the upsert without relying on the incoming ID (which may differ from the
	// server-side ID when the same content was stored independently on two machines).
	hashAttrs := make(map[string]map[string]string, len(rows))
	for _, m := range rows {
		if a := attrs[m.ID]; len(a) > 0 {
			hashAttrs[m.ContentHash] = a
		}
	}

	err = d.WithTx(ctx, func(tx *sql.Tx) error {
		for _, m := range rows {
			res, err := tx.ExecContext(ctx, `
INSERT INTO memories
	(id, layer, content, content_hash, embedding, importance, access_count,
	 created_at, updated_at, last_accessed_at, decay_rate, stability, source, agent,
	 valence, valence_scored, embedding_model)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(content_hash) DO UPDATE SET
	updated_at      = excluded.updated_at,
	importance      = excluded.importance,
	valence         = excluded.valence,
	valence_scored  = excluded.valence_scored,
	embedding       = CASE WHEN length(excluded.embedding) > 0 THEN excluded.embedding ELSE memories.embedding END,
	embedding_model = excluded.embedding_model,
	decay_rate      = excluded.decay_rate,
	stability       = excluded.stability
WHERE excluded.updated_at > memories.updated_at`,
				m.ID, m.Layer, m.Content, m.ContentHash, m.Embedding, m.Importance, m.AccessCount,
				m.CreatedAt.UTC(), m.UpdatedAt.UTC(), m.LastAccessedAt.UTC(),
				m.DecayRate, m.Stability, m.Source, m.Agent,
				m.Valence, m.ValenceScored, m.EmbeddingModel,
			)
			if err != nil {
				return fmt.Errorf("upsert memory %q: %w", m.ID, err)
			}
			n, err := res.RowsAffected()
			if err != nil {
				return fmt.Errorf("rows affected %q: %w", m.ID, err)
			}
			if n == 0 {
				skipped++
				continue
			}

			rowID, err := res.LastInsertId()
			if err != nil {
				return fmt.Errorf("last insert id %q: %w", m.ID, err)
			}

			if len(m.Embedding) > 0 {
				embJSON, err := embeddingBytesToJSON(m.Embedding)
				if err != nil {
					return fmt.Errorf("encode embedding %q: %w", m.ID, err)
				}
				if _, err := tx.ExecContext(ctx,
					`INSERT INTO memories_vec(rowid, embedding) VALUES (?, ?)
					 ON CONFLICT(rowid) DO UPDATE SET embedding = excluded.embedding`,
					rowID, embJSON,
				); err != nil {
					return fmt.Errorf("upsert vec %q: %w", m.ID, err)
				}
			}

			// Resolve the actual stored ID — may differ from m.ID when the same
			// content was stored independently on two machines (conflict on content_hash).
			var actualID string
			if err := tx.QueryRowContext(ctx,
				`SELECT id FROM memories WHERE rowid = ?`, rowID,
			).Scan(&actualID); err != nil {
				return fmt.Errorf("resolve id %q: %w", m.ID, err)
			}

			// Apply attrs using the resolved ID.
			if a := hashAttrs[m.ContentHash]; len(a) > 0 {
				for k, v := range a {
					if _, err := tx.ExecContext(ctx,
						`INSERT INTO memory_attributes(mem_id, key, value) VALUES (?, ?, ?)
						 ON CONFLICT(mem_id, key) DO UPDATE SET value = excluded.value`,
						actualID, k, v,
					); err != nil {
						return fmt.Errorf("upsert attr %q %q: %w", actualID, k, err)
					}
				}
			}

			if actualID == m.ID {
				inserted++
			} else {
				updated++
			}
		}
		return nil
	})
	return inserted, updated, skipped, err
}

// TouchMemory bumps the updated_at timestamp for an existing memory.
// Used when re-storing identical content with new attributes so the change
// propagates on the next sync push.
func (d *DB) TouchMemory(ctx context.Context, id string, t time.Time) error {
	_, err := d.db.ExecContext(ctx,
		`UPDATE memories SET updated_at = ? WHERE id = ?`, t.UTC(), id,
	)
	return err
}

// GetMemory retrieves a memory row by its UUID ID or a unique prefix.
// Returns a wrapped fs.ErrNotExist if no row is found.
// Returns an error if a prefix matches more than one row.
func (d *DB) GetMemory(ctx context.Context, id string) (*MemoryRow, error) {
	// Full UUID — exact match.
	if len(id) == 36 {
		row := d.db.QueryRowContext(ctx, `
SELECT id, layer, content, content_hash, embedding, importance, access_count,
       created_at, updated_at, last_accessed_at, decay_rate, stability, source, agent, compacted_at,
       valence, valence_scored, embedding_model
FROM memories WHERE id = ?`, id)
		m, err := scanMemoryRow(row)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, fmt.Errorf("memory %q: %w", id, fs.ErrNotExist)
			}
			return nil, fmt.Errorf("get memory: %w", err)
		}
		return m, nil
	}

	// Prefix match — collect all candidates.
	rows, err := d.db.QueryContext(ctx, `
SELECT id, layer, content, content_hash, embedding, importance, access_count,
       created_at, updated_at, last_accessed_at, decay_rate, stability, source, agent, compacted_at,
       valence, valence_scored, embedding_model
FROM memories WHERE id LIKE ?`, id+"%")
	if err != nil {
		return nil, fmt.Errorf("get memory prefix: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	matches, err := scanMemoryRows(rows)
	if err != nil {
		return nil, fmt.Errorf("scan memory prefix: %w", err)
	}
	switch len(matches) {
	case 0:
		return nil, fmt.Errorf("memory %q: %w", id, fs.ErrNotExist)
	case 1:
		return matches[0], nil
	default:
		return nil, fmt.Errorf("prefix %q matches %d memories — use more characters", id, len(matches))
	}
}

// GetByHash retrieves a memory row by its content hash.
// Returns a wrapped fs.ErrNotExist if no row is found.
func (d *DB) GetByHash(ctx context.Context, hash string) (*MemoryRow, error) {
	row := d.db.QueryRowContext(ctx, `
SELECT id, layer, content, content_hash, embedding, importance, access_count,
       created_at, updated_at, last_accessed_at, decay_rate, stability, source, agent, compacted_at,
       valence, valence_scored, embedding_model
FROM memories WHERE content_hash = ?`, hash)

	m, err := scanMemoryRow(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("memory hash %q: %w", hash, fs.ErrNotExist)
		}
		return nil, fmt.Errorf("get memory by hash: %w", err)
	}
	return m, nil
}

// MarkAccessed increments access_count and sets last_accessed_at for the given memory ID.
func (d *DB) MarkAccessed(ctx context.Context, id string, now time.Time) error {
	_, err := d.db.ExecContext(ctx, `
UPDATE memories SET access_count = access_count + 1, last_accessed_at = ? WHERE id = ?`,
		now.UTC(), id)
	if err != nil {
		return fmt.Errorf("mark accessed: %w", err)
	}
	return nil
}

// SoftDelete sets compacted_at on the given memory row without deleting the row.
func (d *DB) SoftDelete(ctx context.Context, id string, at time.Time) error {
	_, err := d.db.ExecContext(ctx, `
UPDATE memories SET compacted_at = ? WHERE id = ?`, at.UTC(), id)
	if err != nil {
		return fmt.Errorf("soft delete: %w", err)
	}
	return nil
}

// ListLayer returns all memories in the given layer.
// If activeOnly is true, only rows where compacted_at IS NULL are returned.
func (d *DB) ListLayer(ctx context.Context, layer int, activeOnly bool) ([]*MemoryRow, error) {
	query := `
SELECT id, layer, content, content_hash, embedding, importance, access_count,
       created_at, updated_at, last_accessed_at, decay_rate, stability, source, agent, compacted_at,
       valence, valence_scored, embedding_model
FROM memories WHERE layer = ?`
	if activeOnly {
		query += ` AND compacted_at IS NULL`
	}
	query += ` ORDER BY created_at ASC`

	rows, err := d.db.QueryContext(ctx, query, layer)
	if err != nil {
		return nil, fmt.Errorf("list layer: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	return scanMemoryRows(rows)
}

// CountByLayer returns the number of active (non-compacted) memories in the given layer.
func (d *DB) CountByLayer(ctx context.Context, layer int) (int, error) {
	var count int
	err := d.db.QueryRowContext(ctx, `
SELECT COUNT(*) FROM memories WHERE layer = ? AND compacted_at IS NULL`, layer).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count by layer: %w", err)
	}
	return count, nil
}

// OldestByLayer returns the created_at of the oldest active memory in the given layer.
// Returns nil if no active memories exist.
func (d *DB) OldestByLayer(ctx context.Context, layer int) (*time.Time, error) {
	var ts sql.NullTime
	err := d.db.QueryRowContext(ctx, `
SELECT MIN(created_at) FROM memories WHERE layer = ? AND compacted_at IS NULL`, layer).Scan(&ts)
	if err != nil {
		return nil, fmt.Errorf("oldest by layer: %w", err)
	}
	if !ts.Valid {
		return nil, nil
	}
	t := ts.Time
	return &t, nil
}

// UpdateValence sets the valence field and marks valence_scored=1 for a memory.
// Used by the async valence goroutine and the backfill process.
func (d *DB) UpdateValence(ctx context.Context, id string, valence float32) error {
	_, err := d.db.ExecContext(ctx,
		`UPDATE memories SET valence = ?, valence_scored = 1 WHERE id = ?`,
		valence, id)
	if err != nil {
		return fmt.Errorf("update valence: %w", err)
	}
	return nil
}

// ListUnscored returns up to limit memories where valence_scored=false and compacted_at IS NULL.
// Used by the backfill goroutine in the daemon.
func (d *DB) ListUnscored(ctx context.Context, limit int) ([]*MemoryRow, error) {
	rows, err := d.db.QueryContext(ctx, `
SELECT id, layer, content, content_hash, embedding, importance, access_count,
       created_at, updated_at, last_accessed_at, decay_rate, stability, source, agent, compacted_at,
       valence, valence_scored, embedding_model
FROM memories
WHERE valence_scored = 0 AND compacted_at IS NULL
ORDER BY created_at ASC
LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("list unscored: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	return scanMemoryRows(rows)
}

// ListUnimportant returns up to limit memories where importance equals the default 5.0,
// indicating the LLM has not yet scored them. Used by the BackfillImportance goroutine.
func (d *DB) ListUnimportant(ctx context.Context, limit int) ([]*MemoryRow, error) {
	rows, err := d.db.QueryContext(ctx, `
SELECT id, layer, content, content_hash, embedding, importance, access_count,
       created_at, updated_at, last_accessed_at, decay_rate, stability, source, agent, compacted_at,
       valence, valence_scored, embedding_model
FROM memories
WHERE importance = 5.0 AND compacted_at IS NULL
ORDER BY created_at ASC
LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("list unimportant: %w", err)
	}
	defer rows.Close() //nolint:errcheck
	return scanMemoryRows(rows)
}

// ListUntagged returns up to limit memories that have no 'tags' attribute,
// indicating the LLM has not yet extracted tags. Used by the BackfillTags goroutine.
func (d *DB) ListUntagged(ctx context.Context, limit int) ([]*MemoryRow, error) {
	rows, err := d.db.QueryContext(ctx, `
SELECT id, layer, content, content_hash, embedding, importance, access_count,
       created_at, updated_at, last_accessed_at, decay_rate, stability, source, agent, compacted_at,
       valence, valence_scored, embedding_model
FROM memories
WHERE compacted_at IS NULL
  AND id NOT IN (SELECT mem_id FROM memory_attributes WHERE key = 'tags')
ORDER BY created_at ASC
LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("list untagged: %w", err)
	}
	defer rows.Close() //nolint:errcheck
	return scanMemoryRows(rows)
}

// ListUnembedded returns up to limit memories that need embedding: either no embedding exists,
// or the embedding was produced by a different model. Used by BackfillEmbeddings.
func (d *DB) ListUnembedded(ctx context.Context, limit int, model string) ([]*MemoryRow, error) {
	rows, err := d.db.QueryContext(ctx, `
SELECT id, layer, content, content_hash, embedding, importance, access_count,
       created_at, updated_at, last_accessed_at, decay_rate, stability, source, agent, compacted_at,
       valence, valence_scored, embedding_model
FROM memories
WHERE compacted_at IS NULL
  AND (embedding IS NULL OR length(embedding) = 0 OR embedding_model != ?)
ORDER BY created_at ASC
LIMIT ?`, model, limit)
	if err != nil {
		return nil, fmt.Errorf("list unembedded: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	return scanMemoryRows(rows)
}

// UpdateEmbedding sets the embedding field for a memory. Used by the BackfillEmbeddings goroutine.
func (d *DB) UpdateEmbedding(ctx context.Context, id string, embedding []byte, model string) error {
	_, err := d.db.ExecContext(ctx,
		`UPDATE memories SET embedding = ?, embedding_model = ? WHERE id = ?`,
		embedding, model, id)
	return err
}

// UpdateImportance sets the importance field for a memory. Used by the async importance goroutine.
func (d *DB) UpdateImportance(ctx context.Context, id string, importance float32) error {
	_, err := d.db.ExecContext(ctx, `UPDATE memories SET importance = ? WHERE id = ?`, importance, id)
	if err != nil {
		return fmt.Errorf("update importance: %w", err)
	}
	return nil
}

// UpdateStability updates the stability and decay_rate fields for a memory.
func (d *DB) UpdateStability(ctx context.Context, id string, stability, decayRate float32) error {
	_, err := d.db.ExecContext(ctx, `
UPDATE memories SET stability = ?, decay_rate = ? WHERE id = ?`, stability, decayRate, id)
	if err != nil {
		return fmt.Errorf("update stability: %w", err)
	}
	return nil
}

// scanMemoryRow scans a single row into a MemoryRow.
func scanMemoryRow(row *sql.Row) (*MemoryRow, error) {
	m := &MemoryRow{}
	var compactedAt sql.NullTime
	var embBlob []byte

	err := row.Scan(
		&m.ID, &m.Layer, &m.Content, &m.ContentHash, &embBlob, &m.Importance, &m.AccessCount,
		&m.CreatedAt, &m.UpdatedAt, &m.LastAccessedAt, &m.DecayRate, &m.Stability,
		&m.Source, &m.Agent, &compactedAt,
		&m.Valence, &m.ValenceScored, &m.EmbeddingModel,
	)
	if err != nil {
		return nil, err
	}

	m.Embedding = embBlob
	if compactedAt.Valid {
		t := compactedAt.Time
		m.CompactedAt = &t
	}
	return m, nil
}

// scanMemoryRows scans multiple rows into a slice of MemoryRow.
func scanMemoryRows(rows *sql.Rows) ([]*MemoryRow, error) {
	var result []*MemoryRow
	for rows.Next() {
		m := &MemoryRow{}
		var compactedAt sql.NullTime
		var embBlob []byte

		err := rows.Scan(
			&m.ID, &m.Layer, &m.Content, &m.ContentHash, &embBlob, &m.Importance, &m.AccessCount,
			&m.CreatedAt, &m.UpdatedAt, &m.LastAccessedAt, &m.DecayRate, &m.Stability,
			&m.Source, &m.Agent, &compactedAt,
			&m.Valence, &m.ValenceScored, &m.EmbeddingModel,
		)
		if err != nil {
			return nil, fmt.Errorf("scan memory row: %w", err)
		}

		m.Embedding = embBlob
		if compactedAt.Valid {
			t := compactedAt.Time
			m.CompactedAt = &t
		}
		result = append(result, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}
	return result, nil
}

// embeddingBytesToJSON converts raw IEEE 754 little-endian float32 bytes to a JSON array string.
// This is the format expected by vec0's MATCH clause.
func embeddingBytesToJSON(b []byte) (string, error) {
	if len(b)%4 != 0 {
		return "", fmt.Errorf("embedding bytes length %d is not a multiple of 4", len(b))
	}
	n := len(b) / 4
	floats := make([]float32, n)
	for i := range floats {
		bits := binary.LittleEndian.Uint32(b[i*4 : i*4+4])
		floats[i] = math.Float32frombits(bits)
	}
	out, err := json.Marshal(floats)
	if err != nil {
		return "", fmt.Errorf("marshal embedding: %w", err)
	}
	return string(out), nil
}
