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

// InsertMemory inserts a new memory row.
//
// Storage layout: the embedding is written ONLY to the memories_vec virtual table
// (vec0). The memories.embedding BLOB column is always written as NULL — it is
// retained in the schema for backward-compatibility and as a NULL-OR-blob fallback
// read by scan helpers, but vec0 is the authoritative store. Prior to this change
// embeddings were dual-stored, wasting ~3 KB per row (≈180 MB on a 70k-row DB).
//
// If m.Embedding is non-empty and its dimension matches d.embedDim, it is inserted
// into memories_vec. If the dimension does not match (e.g. imported from a machine
// using a different embedding model), the vec insert is skipped and BackfillEmbeddings
// will re-embed with the configured model.
func (d *DB) InsertMemory(ctx context.Context, m *MemoryRow) error {
	if len(m.Embedding) > 0 {
		if err := d.ensureVecTable(ctx, len(m.Embedding)/4); err != nil {
			return fmt.Errorf("ensure vec table: %w", err)
		}
	}
	return d.WithTx(ctx, func(tx *sql.Tx) error {
		// Note: the embedding column is written as NULL on purpose — vec0 owns the
		// vector. Scan helpers fall back to vec0 by id when the column is NULL.
		res, err := tx.ExecContext(ctx, `
INSERT INTO memories
	(id, layer, content, content_hash, embedding, importance, access_count,
	 created_at, updated_at, last_accessed_at, decay_rate, stability, source, agent,
	 valence, valence_scored, embedding_model)
VALUES (?, ?, ?, ?, NULL, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			m.ID, m.Layer, m.Content, m.ContentHash, m.Importance, m.AccessCount,
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
			if err := d.ensureVecTable(ctx, len(m.Embedding)/4); err != nil {
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
			// Insert NULL for embedding column — vec0 is authoritative. See InsertMemory doc.
			res, err := tx.ExecContext(ctx, `
INSERT INTO memories
	(id, layer, content, content_hash, embedding, importance, access_count,
	 created_at, updated_at, last_accessed_at, decay_rate, stability, source, agent,
	 valence, valence_scored, embedding_model)
VALUES (?, ?, ?, ?, NULL, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(content_hash) DO UPDATE SET
	updated_at      = excluded.updated_at,
	importance      = excluded.importance,
	valence         = excluded.valence,
	valence_scored  = excluded.valence_scored,
	embedding_model = excluded.embedding_model,
	decay_rate      = excluded.decay_rate,
	stability       = excluded.stability
WHERE excluded.updated_at > memories.updated_at`,
				m.ID, m.Layer, m.Content, m.ContentHash, m.Importance, m.AccessCount,
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

			// Resolve the actual stored ID and rowid via content_hash — reliable for both
			// INSERT (new row) and UPDATE (conflict resolved) paths.
			// LastInsertId() is unreliable for UPDATE in some SQLite drivers.
			var actualID string
			var rowID int64
			if err := tx.QueryRowContext(ctx,
				`SELECT id, rowid FROM memories WHERE content_hash = ?`, m.ContentHash,
			).Scan(&actualID, &rowID); err != nil {
				return fmt.Errorf("resolve id %q: %w", m.ID, err)
			}

			if len(m.Embedding) > 0 {
				embJSON, err := embeddingBytesToJSON(m.Embedding)
				if err != nil {
					return fmt.Errorf("encode embedding %q: %w", m.ID, err)
				}
				// vec0 does not support ON CONFLICT UPSERT or INSERT OR REPLACE.
				// Emulate via UPDATE-then-INSERT (see UpdateEmbedding).
				res, err := tx.ExecContext(ctx,
					`UPDATE memories_vec SET embedding = ? WHERE rowid = ?`, embJSON, rowID,
				)
				if err != nil {
					return fmt.Errorf("update vec %q: %w", m.ID, err)
				}
				if n, _ := res.RowsAffected(); n == 0 {
					if _, err := tx.ExecContext(ctx,
						`INSERT INTO memories_vec(rowid, embedding) VALUES (?, ?)`,
						rowID, embJSON,
					); err != nil {
						return fmt.Errorf("insert vec %q: %w", m.ID, err)
					}
				}
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
		m, err := d.scanMemoryRow(ctx, row)
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

	matches, err := d.scanMemoryRows(ctx, rows)
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

	m, err := d.scanMemoryRow(ctx, row)
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

	return d.scanMemoryRows(ctx, rows)
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

	return d.scanMemoryRows(ctx, rows)
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
	return d.scanMemoryRows(ctx, rows)
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
	return d.scanMemoryRows(ctx, rows)
}

// ListUnembedded returns up to limit active memories that need embedding: either
// no row exists in memories_vec for them, or their embedding was produced by a
// different model. Used by BackfillEmbeddings.
//
// Note: this checks memories_vec membership by rowid, NOT the legacy
// memories.embedding BLOB column. After the dual-store-removal migration, the BLOB
// is always NULL for vec0-present rows, so checking the BLOB would falsely return
// every active row. Vec0 is authoritative; vec0 membership is the source of truth.
func (d *DB) ListUnembedded(ctx context.Context, limit int, model string) ([]*MemoryRow, error) {
	rows, err := d.db.QueryContext(ctx, `
SELECT id, layer, content, content_hash, embedding, importance, access_count,
       created_at, updated_at, last_accessed_at, decay_rate, stability, source, agent, compacted_at,
       valence, valence_scored, embedding_model
FROM memories
WHERE compacted_at IS NULL
  AND (rowid NOT IN (SELECT rowid FROM memories_vec) OR embedding_model != ?)
ORDER BY created_at ASC
LIMIT ?`, model, limit)
	if err != nil {
		return nil, fmt.Errorf("list unembedded: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	return d.scanMemoryRows(ctx, rows)
}

// UpdateEmbedding upserts the embedding for a memory into memories_vec and updates
// embedding_model on the memories row. The memories.embedding BLOB column is set
// to NULL (vec0 is authoritative; see InsertMemory).
//
// Used by the BackfillEmbeddings goroutine to fill in embeddings for rows that
// were inserted without one, or whose embedding model differs from the current one.
//
// Prior to this change UpdateEmbedding only wrote the BLOB column, which meant
// backfilled embeddings never reached vec0 — a latent bug that made backfilled
// rows invisible to vector search.
func (d *DB) UpdateEmbedding(ctx context.Context, id string, embedding []byte, model string) error {
	if len(embedding) == 0 {
		return fmt.Errorf("update embedding: empty embedding for id %q", id)
	}
	if d.embedDim > 0 && len(embedding)/4 != d.embedDim {
		return fmt.Errorf("update embedding: dim %d does not match configured %d for id %q", len(embedding)/4, d.embedDim, id)
	}
	// Lazily create memories_vec if Open was called with embedDim=0. Cached after
	// the first successful create so we don't pay the DDL cost on every call —
	// CREATE TABLE IF NOT EXISTS is cheap when the table exists but still takes a
	// reserved lock that serializes with concurrent enrichment goroutines.
	dim := len(embedding) / 4
	if err := d.ensureVecTable(ctx, dim); err != nil {
		return fmt.Errorf("ensure vec table: %w", err)
	}
	// Encode outside the transaction — it's pure CPU and shouldn't hold the lock.
	embJSON, err := embeddingBytesToJSON(embedding)
	if err != nil {
		return fmt.Errorf("encode embedding: %w", err)
	}

	// vec0 does not support ON CONFLICT UPSERT or INSERT OR REPLACE on virtual
	// tables. We emulate upsert as UPDATE-then-INSERT: try UPDATE first; if no
	// row was affected, INSERT instead. Both happen inside one transaction so
	// concurrent writers don't observe a missing row between the two statements.
	return d.WithTx(ctx, func(tx *sql.Tx) error {
		// Resolve rowid via id (unique index on memories.id makes this O(log n)).
		var rowID int64
		if err := tx.QueryRowContext(ctx, `SELECT rowid FROM memories WHERE id = ?`, id).Scan(&rowID); err != nil {
			return fmt.Errorf("resolve rowid for embedding update: %w", err)
		}

		// Try UPDATE. If it touches 0 rows, the vec0 entry doesn't exist yet — INSERT.
		res, err := tx.ExecContext(ctx,
			`UPDATE memories_vec SET embedding = ? WHERE rowid = ?`, embJSON, rowID,
		)
		if err != nil {
			return fmt.Errorf("update vec embedding: %w", err)
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO memories_vec(rowid, embedding) VALUES (?, ?)`,
				rowID, embJSON,
			); err != nil {
				return fmt.Errorf("insert vec embedding: %w", err)
			}
		}

		if _, err := tx.ExecContext(ctx,
			`UPDATE memories SET embedding = NULL, embedding_model = ? WHERE id = ?`,
			model, id,
		); err != nil {
			return fmt.Errorf("update embedding_model: %w", err)
		}
		return nil
	})
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

// scanMemoryRow scans a single row into a MemoryRow. If the embedding BLOB column
// is NULL/empty, it falls back to fetching the embedding from memories_vec by id.
// This makes the dual-store-removal migration (which NULLs the BLOB column for all
// vec0-present rows) transparent to all callers — m.Embedding is still populated.
//
// Cost: at most one extra single-row query against vec0 when the BLOB is NULL.
func (d *DB) scanMemoryRow(ctx context.Context, row *sql.Row) (*MemoryRow, error) {
	m, err := scanMemoryRowRaw(row)
	if err != nil {
		return nil, err
	}
	if len(m.Embedding) == 0 {
		emb, ferr := d.fetchVecEmbedding(ctx, m.ID)
		if ferr == nil && len(emb) > 0 {
			m.Embedding = emb
		}
		// On error (e.g. row not in vec0): leave m.Embedding empty. Callers already
		// handle len(m.Embedding) == 0 as "no embedding available".
	}
	return m, nil
}

// scanMemoryRows scans multiple rows. After the initial scan, it issues a single
// batched query against memories_vec for any rows with NULL embedding and fills
// them in. This avoids the N+1 problem that would arise from per-row fallback.
func (d *DB) scanMemoryRows(ctx context.Context, rows *sql.Rows) ([]*MemoryRow, error) {
	result, err := scanMemoryRowsRaw(rows)
	if err != nil {
		return nil, err
	}
	if len(result) == 0 {
		return result, nil
	}

	// Collect ids of rows that need a vec0 fallback fetch.
	needed := make([]string, 0, len(result))
	index := make(map[string]int, len(result))
	for i, m := range result {
		if len(m.Embedding) == 0 {
			needed = append(needed, m.ID)
			index[m.ID] = i
		}
	}
	if len(needed) == 0 {
		return result, nil
	}

	embs, err := d.fetchVecEmbeddingsBatch(ctx, needed)
	if err != nil {
		// Non-fatal: log via wrapping. Callers handle len(m.Embedding)==0 gracefully.
		return result, nil //nolint:nilerr // intentional: fallback fetch errors must not break list ops
	}
	for id, emb := range embs {
		if i, ok := index[id]; ok {
			result[i].Embedding = emb
		}
	}
	return result, nil
}

// scanMemoryRowRaw is the unwrapped per-row scan with no fallback. Used internally
// by scanMemoryRow and scanMemoryRows; also by code paths that don't need the
// embedding bytes populated.
func scanMemoryRowRaw(row *sql.Row) (*MemoryRow, error) {
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

func scanMemoryRowsRaw(rows *sql.Rows) ([]*MemoryRow, error) {
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

// fetchVecEmbedding fetches the embedding for a single memory id from memories_vec
// via the JOIN m.rowid = mv.rowid. Returns nil bytes (no error) when the id has
// no vec0 entry, so the row is treated as "no embedding available" by callers.
func (d *DB) fetchVecEmbedding(ctx context.Context, id string) ([]byte, error) {
	var emb []byte
	err := d.db.QueryRowContext(ctx, `
SELECT mv.embedding FROM memories_vec mv
JOIN memories m ON m.rowid = mv.rowid
WHERE m.id = ?`, id).Scan(&emb)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return emb, err
}

// fetchVecEmbeddingsBatch is the batch version of fetchVecEmbedding. Issues a
// single query of the form: WHERE m.id IN (?, ?, ...). Returns a map of id to
// embedding bytes containing only the ids that had a vec0 entry.
func (d *DB) fetchVecEmbeddingsBatch(ctx context.Context, ids []string) (map[string][]byte, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	placeholders := make([]byte, 0, 2*len(ids)-1)
	args := make([]any, len(ids))
	for i, id := range ids {
		if i > 0 {
			placeholders = append(placeholders, ',')
		}
		placeholders = append(placeholders, '?')
		args[i] = id
	}
	//nolint:gosec // placeholders is a fixed pattern of '?' and ','; args carry user data
	query := `
SELECT m.id, mv.embedding FROM memories_vec mv
JOIN memories m ON m.rowid = mv.rowid
WHERE m.id IN (` + string(placeholders) + `)`

	rows, err := d.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("fetch vec embeddings batch: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	out := make(map[string][]byte, len(ids))
	for rows.Next() {
		var id string
		var emb []byte
		if err := rows.Scan(&id, &emb); err != nil {
			return nil, fmt.Errorf("scan vec embedding: %w", err)
		}
		out[id] = emb
	}
	return out, rows.Err()
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
