// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// Package db provides SQLite connection lifecycle, schema management, and raw CRUD operations.
package db

import (
	"context"
	"database/sql"
	"fmt"
	"sync"

	_ "modernc.org/sqlite"     // register sqlite driver
	_ "modernc.org/sqlite/vec" // register vec0 virtual table
)

// DB wraps a SQLite connection pool.
type DB struct {
	db       *sql.DB
	embedDim int // expected embedding dimension; 0 means unknown

	// vecCreatedMu protects vecCreatedDim. UpdateEmbedding may lazily create
	// memories_vec the first time an embedding of a given dimension arrives
	// (when Open was called with embedDim=0). We cache the dim so subsequent
	// calls skip the CREATE TABLE IF NOT EXISTS DDL — it is cheap when the
	// table exists, but still acquires SQLite's reserved lock and serialized
	// with concurrent enrichment goroutines, slowing them down enough to lose
	// races against immediate compactor reads.
	vecCreatedMu  sync.Mutex
	vecCreatedDim int
}

// Open opens (or creates) the SQLite database at path with WAL mode, foreign keys,
// and busy timeout enabled. It creates the schema if it does not exist.
func Open(path string, embedDim int) (*DB, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)", path)
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	sqlDB.SetMaxOpenConns(1)

	// Verify WAL mode was actually applied; some filesystems silently ignore it.
	var journalMode string
	if err := sqlDB.QueryRowContext(context.Background(), "PRAGMA journal_mode").Scan(&journalMode); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("check WAL mode: %w", err)
	}
	if journalMode != "wal" {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("WAL mode not applied, got %q", journalMode)
	}

	d := &DB{db: sqlDB, embedDim: embedDim}
	if embedDim > 0 {
		d.vecCreatedDim = embedDim // createSchema will create memories_vec eagerly
	}
	if err := d.createSchema(embedDim); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("create schema: %w", err)
	}

	// When embedDim > 0, check if vec table dimension matches config; recreate if not.
	if embedDim > 0 {
		if err := d.ensureVecDim(embedDim); err != nil {
			_ = sqlDB.Close()
			return nil, fmt.Errorf("ensure vec dim: %w", err)
		}
	}

	return d, nil
}

// MigrateOrphanEmbeddings is the exported entry point for the orphan-embedding
// data migration; see migrate_orphan_embeddings.go. Invoked by `lth maint
// shrink-embeddings`. Idempotent and safe to run while the daemon is up.
func (d *DB) MigrateOrphanEmbeddings(ctx context.Context) error {
	return d.migrateOrphanEmbeddings(ctx)
}

// CountOrphanEmbeddings returns the number of rows where memories.embedding is
// non-NULL but the row has no entry in memories_vec. Used by the maint command
// to report progress.
func (d *DB) CountOrphanEmbeddings(ctx context.Context) (int, error) {
	exists, err := d.tableExists(ctx, "memories_vec")
	if err != nil || !exists {
		return 0, err
	}
	var n int
	err = d.db.QueryRowContext(ctx, `
SELECT COUNT(*) FROM memories m
WHERE m.embedding IS NOT NULL AND m.rowid NOT IN (SELECT rowid FROM memories_vec)`).Scan(&n)
	return n, err
}

// Close closes the underlying database connection.
func (d *DB) Close() error {
	return d.db.Close()
}

// WithTx executes fn within a transaction. It commits on nil return or rolls back on error.
func (d *DB) WithTx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}

	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}

	return nil
}
