// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package backupwatcher

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/mattdurham/lth/internal/config"
	"github.com/mattdurham/lth/internal/db"
)

// makeSnapshot creates a real DB, snapshots it, and returns the snapshot path.
func makeSnapshot(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	d, err := db.Open(filepath.Join(dir, "src.db"), 0)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })

	backupDir := t.TempDir()
	cfg := config.Default()
	cfg.Backup.Dir = backupDir
	w := New(d, cfg, nil)
	if err := w.snapshotOnce(context.Background()); err != nil {
		t.Fatalf("snapshotOnce: %v", err)
	}

	matches, err := filepath.Glob(filepath.Join(backupDir, "memory-*.db.gz"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("expected exactly one snapshot, got %v (err=%v)", matches, err)
	}
	return matches[0]
}

func TestRestoreFreshDatabase(t *testing.T) {
	snapshot := makeSnapshot(t)
	dbPath := filepath.Join(t.TempDir(), "memory.db")

	preRestore, err := Restore(dbPath, snapshot)
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if preRestore != "" {
		t.Errorf("preRestorePath = %q, want empty (no prior db existed)", preRestore)
	}

	// Restored file should be a valid, openable SQLite database.
	d, err := db.Open(dbPath, 0)
	if err != nil {
		t.Fatalf("open restored db: %v", err)
	}
	d.Close()
}

func TestRestoreMakesPreRestoreCopyAndCleansSidecars(t *testing.T) {
	snapshot := makeSnapshot(t)

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "memory.db")
	if err := os.WriteFile(dbPath, []byte("old database content"), 0o600); err != nil {
		t.Fatalf("write existing db: %v", err)
	}
	if err := os.WriteFile(dbPath+"-wal", []byte("old wal"), 0o600); err != nil {
		t.Fatalf("write existing wal: %v", err)
	}
	if err := os.WriteFile(dbPath+"-shm", []byte("old shm"), 0o600); err != nil {
		t.Fatalf("write existing shm: %v", err)
	}

	preRestore, err := Restore(dbPath, snapshot)
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if preRestore != dbPath+".pre-restore" {
		t.Errorf("preRestorePath = %q, want %q", preRestore, dbPath+".pre-restore")
	}

	preData, err := os.ReadFile(preRestore)
	if err != nil {
		t.Fatalf("read pre-restore copy: %v", err)
	}
	if string(preData) != "old database content" {
		t.Errorf("pre-restore copy content = %q, want the original db content", preData)
	}
	if _, err := os.Stat(dbPath + "-wal.pre-restore"); err != nil {
		t.Errorf("expected -wal sidecar to also be pre-restore-copied: %v", err)
	}
	if _, err := os.Stat(dbPath + "-shm.pre-restore"); err != nil {
		t.Errorf("expected -shm sidecar to also be pre-restore-copied: %v", err)
	}

	// Stale sidecars from the OLD database must not survive the restore.
	if _, err := os.Stat(dbPath + "-wal"); !os.IsNotExist(err) {
		t.Errorf("stale -wal sidecar should have been removed after restore, stat err = %v", err)
	}
	if _, err := os.Stat(dbPath + "-shm"); !os.IsNotExist(err) {
		t.Errorf("stale -shm sidecar should have been removed after restore, stat err = %v", err)
	}

	// The restored file itself should be the new snapshot's content, not the old one.
	d, err := db.Open(dbPath, 0)
	if err != nil {
		t.Fatalf("open restored db: %v", err)
	}
	d.Close()
}

func TestListSnapshotsOrderedOldestFirst(t *testing.T) {
	dir := t.TempDir()
	names := []string{
		"memory-20260103-000000.db.gz",
		"memory-20260101-000000.db.gz",
		"memory-20260102-000000.db.gz",
	}
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o600); err != nil {
			t.Fatalf("write %s: %v", n, err)
		}
	}

	snaps, err := ListSnapshots(dir)
	if err != nil {
		t.Fatalf("ListSnapshots: %v", err)
	}
	if len(snaps) != 3 {
		t.Fatalf("got %d snapshots, want 3", len(snaps))
	}
	wantOrder := []string{
		"memory-20260101-000000.db.gz",
		"memory-20260102-000000.db.gz",
		"memory-20260103-000000.db.gz",
	}
	for i, want := range wantOrder {
		if snaps[i].Name != want {
			t.Errorf("snaps[%d].Name = %q, want %q", i, snaps[i].Name, want)
		}
	}
	if snaps[0].Time.After(snaps[2].Time) {
		t.Errorf("parsed times not in chronological order: %v vs %v", snaps[0].Time, snaps[2].Time)
	}
}

func TestListSnapshotsEmptyDir(t *testing.T) {
	snaps, err := ListSnapshots(t.TempDir())
	if err != nil {
		t.Fatalf("ListSnapshots: %v", err)
	}
	if len(snaps) != 0 {
		t.Errorf("snaps = %v, want empty", snaps)
	}
}
