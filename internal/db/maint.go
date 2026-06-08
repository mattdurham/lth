// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package db

import (
	"context"
	"errors"
	"fmt"
)

// ErrCheckpointBusy is returned by WALCheckpointTruncate when SQLite could not
// fully truncate the WAL because another connection held it open. The data is
// safe — pending WAL pages have been flushed to the main file — but the .db-wal
// sidecar was not zeroed. Callers can ignore this (the next idle checkpoint
// will succeed) or retry. Detected via errors.Is.
var ErrCheckpointBusy = errors.New("wal checkpoint busy: another connection holds the WAL open")

// ensureVecTable creates memories_vec lazily for the given embedding dimension.
// It caches the dim after the first successful creation so subsequent calls
// become a single mutex acquisition and a fast equality check — avoiding the
// SQLite reserved lock that CREATE TABLE IF NOT EXISTS would otherwise take on
// every call. This is on the hot path for UpdateEmbedding which can run hundreds
// of times per second during backfill.
//
// Returns an error if the dimension differs from a previously-seen dimension on
// the same DB connection (which would indicate a config mismatch — the dim of
// memories_vec is fixed at creation time).
func (d *DB) ensureVecTable(ctx context.Context, dim int) error {
	if dim <= 0 {
		return fmt.Errorf("ensure vec table: invalid dim %d", dim)
	}
	d.vecCreatedMu.Lock()
	defer d.vecCreatedMu.Unlock()
	if d.vecCreatedDim == dim {
		return nil
	}
	if d.vecCreatedDim != 0 && d.vecCreatedDim != dim {
		return fmt.Errorf("ensure vec table: dim mismatch (have %d, want %d)", d.vecCreatedDim, dim)
	}
	if _, err := d.db.ExecContext(ctx, fmt.Sprintf(schemaVec, dim)); err != nil {
		return fmt.Errorf("create memories_vec(dim=%d): %w", dim, err)
	}
	d.vecCreatedDim = dim
	return nil
}

// WALCheckpointTruncate runs `PRAGMA wal_checkpoint(TRUNCATE)`. This copies any
// pending WAL pages into the main database file, waits briefly for active
// readers/writers, then truncates the .db-wal sidecar back to zero bytes on disk.
//
// SQLite's default automatic checkpointer (wal_autocheckpoint=1000 pages, PASSIVE
// mode) reuses WAL pages in place but never shrinks the WAL file. A long-lived
// daemon may accumulate a multi-megabyte WAL that never shrinks back. Calling this
// periodically from an idle daemon keeps the WAL bounded.
//
// Returns the number of WAL pages that existed and the number that were
// checkpointed. The third value from PRAGMA (busy=0 means success) is intentionally
// not returned; errors propagate via err.
//
// Safe to call concurrently with reads. Briefly serializes with writers.
func (d *DB) WALCheckpointTruncate(ctx context.Context) (walPages, checkpointed int, err error) {
	// PRAGMA wal_checkpoint returns three columns: (busy, log, checkpointed).
	// busy=0 means the checkpoint completed and the .db-wal file was truncated.
	// busy=1 means another connection held the WAL open and prevented truncate;
	// in this case PRAGMA returns (1, -1, -1) and the WAL stays at its current
	// size, but the main file is still consistent. We surface that as a sentinel
	// error (ErrCheckpointBusy) so callers can distinguish "real failure" from
	// "please retry when no other connection is active".
	var busy int
	if err := d.db.QueryRowContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`).Scan(&busy, &walPages, &checkpointed); err != nil {
		return 0, 0, fmt.Errorf("wal_checkpoint(TRUNCATE): %w", err)
	}
	if busy != 0 {
		return walPages, checkpointed, ErrCheckpointBusy
	}
	return walPages, checkpointed, nil
}

// Vacuum runs `VACUUM` on the database. This rebuilds the .db file from scratch,
// reclaiming all free pages and defragmenting on-disk layout. After large deletions
// or migrations that NULL out blobs, the file size only shrinks if VACUUM is run.
//
// VACUUM requires a temporary copy of the database (so transient disk usage
// roughly doubles) and acquires an exclusive lock for the duration. It must run
// outside of any open transaction.
//
// Returns the database file sizes (bytes) before and after.
func (d *DB) Vacuum(ctx context.Context) (beforeBytes, afterBytes int64, err error) {
	if err := d.db.QueryRowContext(ctx, `SELECT page_count * page_size FROM pragma_page_count, pragma_page_size`).Scan(&beforeBytes); err != nil {
		return 0, 0, fmt.Errorf("query size before vacuum: %w", err)
	}
	if _, err := d.db.ExecContext(ctx, `VACUUM`); err != nil {
		return beforeBytes, 0, fmt.Errorf("vacuum: %w", err)
	}
	if err := d.db.QueryRowContext(ctx, `SELECT page_count * page_size FROM pragma_page_count, pragma_page_size`).Scan(&afterBytes); err != nil {
		return beforeBytes, 0, fmt.Errorf("query size after vacuum: %w", err)
	}
	return beforeBytes, afterBytes, nil
}
