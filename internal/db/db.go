// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// Package db provides SQLite connection lifecycle, schema management, and raw CRUD operations.
package db

import (
	"context"
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"     // register sqlite driver
	_ "modernc.org/sqlite/vec" // register vec0 virtual table
)

// DB wraps a SQLite connection pool.
type DB struct {
	db *sql.DB
}

// Open opens (or creates) the SQLite database at path with WAL mode, foreign keys,
// and busy timeout enabled. It creates the schema if it does not exist.
func Open(path string) (*DB, error) {
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

	d := &DB{db: sqlDB}
	if err := d.createSchema(); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("create schema: %w", err)
	}

	return d, nil
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
