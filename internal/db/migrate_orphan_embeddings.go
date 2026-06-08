// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package db

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"
)

// migrateOrphanEmbeddings finds rows where memories.embedding is non-NULL but the
// row has no corresponding entry in memories_vec, inserts the embedding into vec0,
// and then NULLs the BLOB.
//
// These "orphan" rows are a legacy of the now-fixed UpdateEmbedding bug: the old
// implementation wrote embeddings only to the BLOB column, never to vec0, so any
// embedding produced by BackfillEmbeddings was invisible to vector search. The
// audit of a 488 MB production database found ~51k such orphans (~155 MB of
// BLOBs stranded outside vec0).
//
// Runs on every Open, but is idempotent and quick after the first run: the SELECT
// returns zero rows once all orphans have been moved.
//
// Processed in batches of orphanBatchSize to bound transaction size and keep
// concurrent reads responsive.
func (d *DB) migrateOrphanEmbeddings(ctx context.Context) error {
	exists, err := d.tableExists(ctx, "memories_vec")
	if err != nil {
		return fmt.Errorf("check memories_vec: %w", err)
	}
	if !exists {
		// No vec0 means no embeddings have ever been inserted; nothing to migrate.
		return nil
	}

	const orphanBatchSize = 500

	total := 0
	start := time.Now()
	for {
		n, err := d.migrateOrphanBatch(ctx, orphanBatchSize)
		if err != nil {
			return fmt.Errorf("migrate orphan batch: %w", err)
		}
		if n == 0 {
			break
		}
		total += n
		if total%5000 == 0 {
			slog.Info("migrating orphan embeddings", "moved", total)
		}
	}
	if total > 0 {
		slog.Info("orphan embedding migration complete", "moved", total, "elapsed", time.Since(start))
	}
	return nil
}

// migrateOrphanBatch processes up to limit orphan rows in one transaction.
// Returns the number of rows actually moved.
//
// An orphan is a row where memories.embedding is non-NULL but no row exists in
// memories_vec for that rowid. The fix is: INSERT into memories_vec, then NULL
// the BLOB. We also ensure the vec0 table has been created for the embedding's
// dimension before attempting the insert (paranoia: memories_vec should already
// exist by the time this runs, but if Open was called with embedDim=0 it may
// not have been created yet).
func (d *DB) migrateOrphanBatch(ctx context.Context, limit int) (int, error) {
	// First, peek at one row's embedding length to determine dim. If there are
	// no orphans, this returns 0 with no error.
	var sampleLen int
	err := d.db.QueryRowContext(ctx, `
SELECT length(m.embedding) FROM memories m
WHERE m.embedding IS NOT NULL
  AND m.rowid NOT IN (SELECT rowid FROM memories_vec)
LIMIT 1`).Scan(&sampleLen)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("sample orphan dim: %w", err)
	}
	if sampleLen%4 != 0 {
		return 0, fmt.Errorf("orphan embedding length %d not multiple of 4", sampleLen)
	}
	dim := sampleLen / 4
	if err := d.ensureVecTable(ctx, dim); err != nil {
		return 0, fmt.Errorf("ensure vec table for orphan dim %d: %w", dim, err)
	}

	// Load a batch of orphans (rowid + embedding bytes) of the matching dim.
	rows, err := d.db.QueryContext(ctx, `
SELECT m.rowid, m.embedding FROM memories m
WHERE m.embedding IS NOT NULL
  AND length(m.embedding) = ?
  AND m.rowid NOT IN (SELECT rowid FROM memories_vec)
LIMIT ?`, sampleLen, limit)
	if err != nil {
		return 0, fmt.Errorf("list orphans: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	type orphan struct {
		rowid int64
		bytes []byte
	}
	var batch []orphan
	for rows.Next() {
		var o orphan
		if err := rows.Scan(&o.rowid, &o.bytes); err != nil {
			return 0, fmt.Errorf("scan orphan: %w", err)
		}
		batch = append(batch, o)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("orphan rows: %w", err)
	}
	if len(batch) == 0 {
		return 0, nil
	}

	// Apply in one transaction: INSERT vec0 (UPDATE-then-INSERT pattern; see
	// UpdateEmbedding), then NULL the BLOB on the memories row.
	err = d.WithTx(ctx, func(tx *sql.Tx) error {
		for _, o := range batch {
			embJSON, err := embeddingBytesToJSON(o.bytes)
			if err != nil {
				return fmt.Errorf("encode embedding rowid=%d: %w", o.rowid, err)
			}
			// vec0 does not support ON CONFLICT. We listed orphans that are
			// NOT in vec0, so a plain INSERT should succeed; but use the
			// UPDATE-then-INSERT pattern defensively in case of races.
			res, err := tx.ExecContext(ctx,
				`UPDATE memories_vec SET embedding = ? WHERE rowid = ?`, embJSON, o.rowid)
			if err != nil {
				return fmt.Errorf("update vec rowid=%d: %w", o.rowid, err)
			}
			if n, _ := res.RowsAffected(); n == 0 {
				if _, err := tx.ExecContext(ctx,
					`INSERT INTO memories_vec(rowid, embedding) VALUES (?, ?)`,
					o.rowid, embJSON); err != nil {
					return fmt.Errorf("insert vec rowid=%d: %w", o.rowid, err)
				}
			}
			if _, err := tx.ExecContext(ctx,
				`UPDATE memories SET embedding = NULL WHERE rowid = ?`, o.rowid); err != nil {
				return fmt.Errorf("null embedding rowid=%d: %w", o.rowid, err)
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return len(batch), nil
}
