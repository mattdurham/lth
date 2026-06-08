// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package db

import (
	"context"
	"fmt"
)

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
	// busy=0 means the checkpoint completed; non-zero means readers/writers blocked it.
	var busy int
	if err := d.db.QueryRowContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`).Scan(&busy, &walPages, &checkpointed); err != nil {
		return 0, 0, fmt.Errorf("wal_checkpoint(TRUNCATE): %w", err)
	}
	if busy != 0 {
		// TRUNCATE was downgraded internally; data is safe but the WAL file
		// was not zeroed because a reader/writer was active.
		return walPages, checkpointed, fmt.Errorf("wal_checkpoint(TRUNCATE) busy: %d unflushed pages remain", walPages-checkpointed)
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
