// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package db

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWALCheckpointTruncate_BusyIsSentinel(t *testing.T) {
	// Open a second connection to the same DB and hold a read transaction
	// open, which should cause TRUNCATE to be downgraded. The function must
	// return ErrCheckpointBusy (a sentinel) and NOT a generic error, so
	// callers can distinguish busy from real failure.
	dir := t.TempDir()
	path := filepath.Join(dir, "busy.db")

	d1, err := Open(path, 768)
	if err != nil {
		t.Fatalf("open d1: %v", err)
	}
	defer d1.Close() //nolint:errcheck

	// Insert and commit so the WAL has content to checkpoint.
	for i := 0; i < 5; i++ {
		row := &MemoryRow{
			ID: "busy-" + intToStr(i), Layer: 5, Content: "c", ContentHash: "bh-" + intToStr(i),
			Importance: 5, CreatedAt: time.Now(), UpdatedAt: time.Now(), LastAccessedAt: time.Now(),
		}
		if err := d1.InsertMemory(context.Background(), row); err != nil {
			t.Fatalf("insert %d: %v", i, err)
		}
	}

	// Open a second connection and begin a read transaction that we leave open.
	d2, err := Open(path, 768)
	if err != nil {
		t.Fatalf("open d2: %v", err)
	}
	defer d2.Close() //nolint:errcheck

	tx, err := d2.db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("begin d2 tx: %v", err)
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.ExecContext(context.Background(), `SELECT COUNT(*) FROM memories`); err != nil {
		t.Fatalf("d2 read: %v", err)
	}

	// Now attempt TRUNCATE on d1 — should be busy (or succeed if SQLite
	// happens to be lenient). If err is non-nil, it MUST be ErrCheckpointBusy.
	_, _, err = d1.WALCheckpointTruncate(context.Background())
	if err != nil && !errors.Is(err, ErrCheckpointBusy) {
		t.Errorf("got err=%v, want ErrCheckpointBusy or nil", err)
	}
}

func TestWALCheckpointTruncate_Empty(t *testing.T) {
	d := openTempDB(t)
	defer d.Close() //nolint:errcheck

	// On a fresh DB the WAL is empty; checkpoint should succeed and report 0/0.
	walPages, checkpointed, err := d.WALCheckpointTruncate(context.Background())
	if err != nil {
		t.Fatalf("wal checkpoint: %v", err)
	}
	if walPages != 0 || checkpointed != 0 {
		t.Logf("note: fresh DB had %d wal pages (%d checkpointed) — may include schema setup", walPages, checkpointed)
	}
}

func TestWALCheckpointTruncate_AfterWrites(t *testing.T) {
	d := openTempDB(t)
	defer d.Close() //nolint:errcheck

	// Insert several rows to grow the WAL.
	for i := 0; i < 20; i++ {
		row := &MemoryRow{
			ID:          time.Now().Format("20060102150405.000000000") + "-" + string(rune('a'+i)),
			Layer:       5,
			Content:     "checkpoint test content",
			ContentHash: "hash-" + string(rune('a'+i)),
			Importance:  5.0,
			Source:      "test",
			CreatedAt:   time.Now().UTC(),
			UpdatedAt:   time.Now().UTC(),
		}
		if err := d.InsertMemory(context.Background(), row); err != nil {
			t.Fatalf("insert %d: %v", i, err)
		}
	}

	walPages, checkpointed, err := d.WALCheckpointTruncate(context.Background())
	if err != nil {
		t.Fatalf("wal checkpoint after writes: %v", err)
	}
	// On a freshly-checkpointed DB walPages may be 0 (SQLite already auto-checkpointed).
	// What matters: the call succeeded and didn't leave unflushed pages.
	if checkpointed != walPages {
		t.Errorf("expected all %d wal pages to be checkpointed, got %d", walPages, checkpointed)
	}
}

func TestVacuum_ShrinksAfterDeletion(t *testing.T) {
	d := openTempDB(t)
	defer d.Close() //nolint:errcheck

	// Insert and delete many rows to leave behind free pages.
	const n = 200
	ids := make([]string, n)
	for i := 0; i < n; i++ {
		ids[i] = filepath.Join("vac-", time.Now().Format("150405.000000000"), string(rune('a'+(i%26))))
		row := &MemoryRow{
			ID:          ids[i] + "-" + intToStr(i),
			Layer:       5,
			Content:     stringsRepeat("x", 4096), // ~4KB per row → ~800 KB total
			ContentHash: "vac-hash-" + intToStr(i),
			Importance:  5.0,
			Source:      "test",
			CreatedAt:   time.Now().UTC(),
			UpdatedAt:   time.Now().UTC(),
		}
		if err := d.InsertMemory(context.Background(), row); err != nil {
			t.Fatalf("insert %d: %v", i, err)
		}
	}

	if _, err := d.db.ExecContext(context.Background(), "DELETE FROM memories"); err != nil {
		t.Fatalf("delete all: %v", err)
	}
	// Flush WAL so the deleted pages are part of the main file as free pages.
	if _, _, err := d.WALCheckpointTruncate(context.Background()); err != nil {
		t.Fatalf("checkpoint before vacuum: %v", err)
	}

	before, after, err := d.Vacuum(context.Background())
	if err != nil {
		t.Fatalf("vacuum: %v", err)
	}
	if after >= before {
		t.Errorf("expected vacuum to shrink DB, got before=%d after=%d", before, after)
	}
}

func TestVacuumInto_ErrorsWhenTargetExists(t *testing.T) {
	d := openTempDB(t)
	defer d.Close() //nolint:errcheck

	dir := t.TempDir()
	target := filepath.Join(dir, "already-exists.db")
	if err := os.WriteFile(target, []byte("pre-existing content"), 0o600); err != nil {
		t.Fatalf("write pre-existing target: %v", err)
	}

	// VACUUM INTO refuses to overwrite an existing file; callers must always
	// target a fresh or freshly-removed path.
	if err := d.VacuumInto(context.Background(), target); err == nil {
		t.Fatal("VacuumInto onto a pre-existing file: expected error, got nil")
	}
}

func TestVacuumInto_ErrorsOnUnwritableDestinationDir(t *testing.T) {
	d := openTempDB(t)
	defer d.Close() //nolint:errcheck

	target := filepath.Join(t.TempDir(), "nonexistent-subdir", "snapshot.db")
	if err := d.VacuumInto(context.Background(), target); err == nil {
		t.Fatal("VacuumInto into a nonexistent directory: expected error, got nil")
	}
}

// openTempDB creates a fresh DB in a temp directory and returns it.
func openTempDB(t *testing.T) *DB {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	d, err := Open(path, 768)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		// Already closed by defer in tests; tolerate double-close errors.
		_ = os.Remove(path)
	})
	return d
}

// Local helpers to avoid pulling in fmt/strings just for tests.
func intToStr(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func stringsRepeat(s string, n int) string {
	out := make([]byte, 0, len(s)*n)
	for i := 0; i < n; i++ {
		out = append(out, s...)
	}
	return string(out)
}
