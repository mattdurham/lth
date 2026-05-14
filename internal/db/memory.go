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
	return d.WithTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `
INSERT INTO memories
	(id, layer, content, content_hash, embedding, importance, access_count,
	 created_at, updated_at, last_accessed_at, decay_rate, stability, source, agent,
	 valence, valence_scored)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			m.ID, m.Layer, m.Content, m.ContentHash, m.Embedding, m.Importance, m.AccessCount,
			m.CreatedAt.UTC(), m.UpdatedAt.UTC(), m.LastAccessedAt.UTC(),
			m.DecayRate, m.Stability, m.Source, m.Agent,
			m.Valence, m.ValenceScored,
		)
		if err != nil {
			return fmt.Errorf("insert memory: %w", err)
		}

		rowID, err := res.LastInsertId()
		if err != nil {
			return fmt.Errorf("last insert id: %w", err)
		}

		// Only insert into vec table if embedding is provided.
		if len(m.Embedding) > 0 {
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

// GetMemory retrieves a memory row by its UUID ID.
// Returns a wrapped fs.ErrNotExist if no row is found.
func (d *DB) GetMemory(ctx context.Context, id string) (*MemoryRow, error) {
	row := d.db.QueryRowContext(ctx, `
SELECT id, layer, content, content_hash, embedding, importance, access_count,
       created_at, updated_at, last_accessed_at, decay_rate, stability, source, agent, compacted_at,
       valence, valence_scored
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

// GetByHash retrieves a memory row by its content hash.
// Returns a wrapped fs.ErrNotExist if no row is found.
func (d *DB) GetByHash(ctx context.Context, hash string) (*MemoryRow, error) {
	row := d.db.QueryRowContext(ctx, `
SELECT id, layer, content, content_hash, embedding, importance, access_count,
       created_at, updated_at, last_accessed_at, decay_rate, stability, source, agent, compacted_at,
       valence, valence_scored
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
       valence, valence_scored
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
       valence, valence_scored
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
		&m.Valence, &m.ValenceScored,
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
			&m.Valence, &m.ValenceScored,
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
