// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package backupwatcher

import (
	"compress/gzip"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mattdurham/lth/internal/config"
	"github.com/mattdurham/lth/internal/db"
)

// testDB opens a fresh temp-dir database with a couple of rows, so a
// snapshot has something to actually copy.
func testDB(t *testing.T) *db.DB {
	t.Helper()
	dir := t.TempDir()
	d, err := db.Open(filepath.Join(dir, "test.db"), 0)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

func testWatcher(t *testing.T, d *db.DB, backupDir string) *Watcher {
	t.Helper()
	cfg := config.Default()
	cfg.Backup.Dir = backupDir
	cfg.Backup.Keep = 7
	return New(d, cfg, nil)
}

func decompress(t *testing.T, path string) []byte {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gzip reader %s: %v", path, err)
	}
	defer gz.Close()
	data, err := io.ReadAll(gz)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}

func TestSnapshotOnceProducesRestorableGzip(t *testing.T) {
	ctx := context.Background()
	d := testDB(t)
	backupDir := t.TempDir()
	w := testWatcher(t, d, backupDir)

	if err := w.snapshotOnce(ctx); err != nil {
		t.Fatalf("snapshotOnce: %v", err)
	}

	matches, err := filepath.Glob(filepath.Join(backupDir, "memory-*.db.gz"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("found %d snapshot files, want 1: %v", len(matches), matches)
	}

	data := decompress(t, matches[0])
	if len(data) < 100 {
		t.Errorf("decompressed snapshot suspiciously small: %d bytes", len(data))
	}
	// SQLite files start with this fixed 16-byte magic header.
	if string(data[:16]) != "SQLite format 3\x00" {
		t.Errorf("decompressed snapshot does not look like a SQLite file: %q", data[:16])
	}

	// No leftover temp/partial files.
	leftovers, _ := filepath.Glob(filepath.Join(backupDir, ".tmp-memory-*"))
	if len(leftovers) != 0 {
		t.Errorf("leftover temp files after successful snapshot: %v", leftovers)
	}
	partials, _ := filepath.Glob(filepath.Join(backupDir, "*.part"))
	if len(partials) != 0 {
		t.Errorf("leftover partial files after successful snapshot: %v", partials)
	}
}

func TestSnapshotOnceCleansUpStaleFilesFromPriorCrash(t *testing.T) {
	ctx := context.Background()
	d := testDB(t)
	backupDir := t.TempDir()
	w := testWatcher(t, d, backupDir)

	// Simulate a crash mid-VACUUM and mid-gzip on a previous run.
	stale1 := filepath.Join(backupDir, tmpDBPrefix+"20200101-000000.db")
	stale2 := filepath.Join(backupDir, "memory-20200101-000000.db.gz.part")
	for _, p := range []string{stale1, stale2} {
		if err := os.WriteFile(p, []byte("stale"), 0o600); err != nil {
			t.Fatalf("write stale file: %v", err)
		}
	}

	if err := w.snapshotOnce(ctx); err != nil {
		t.Fatalf("snapshotOnce: %v", err)
	}

	for _, p := range []string{stale1, stale2} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("stale file %s should have been cleaned up, stat err = %v", p, err)
		}
	}
}

func TestPruneOldSnapshotsKeepsMostRecentByName(t *testing.T) {
	dir := t.TempDir()
	names := []string{
		"memory-20260101-000000.db.gz",
		"memory-20260102-000000.db.gz",
		"memory-20260103-000000.db.gz",
		"memory-20260104-000000.db.gz",
		"memory-20260105-000000.db.gz",
	}
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o600); err != nil {
			t.Fatalf("write %s: %v", n, err)
		}
	}

	if err := pruneOldSnapshots(dir, 3); err != nil {
		t.Fatalf("pruneOldSnapshots: %v", err)
	}

	remaining, err := filepath.Glob(filepath.Join(dir, "memory-*.db.gz"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(remaining) != 3 {
		t.Fatalf("remaining = %v, want 3 files", remaining)
	}
	for _, want := range names[2:] {
		found := false
		for _, r := range remaining {
			if filepath.Base(r) == want {
				found = true
			}
		}
		if !found {
			t.Errorf("expected %s to survive pruning, remaining = %v", want, remaining)
		}
	}
}

func TestPruneOldSnapshotsNoopWhenUnderLimit(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "memory-20260101-000000.db.gz"), []byte("x"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if err := pruneOldSnapshots(dir, 7); err != nil {
		t.Fatalf("pruneOldSnapshots: %v", err)
	}

	remaining, _ := filepath.Glob(filepath.Join(dir, "memory-*.db.gz"))
	if len(remaining) != 1 {
		t.Errorf("remaining = %v, want the single file untouched", remaining)
	}
}

func TestSnapshotOnceDisabledWithoutDir(t *testing.T) {
	// Run() should never call snapshotOnce when Backup.Dir is empty; this
	// documents that snapshotOnce itself has no special-case for an empty
	// dir (it would try to MkdirAll("") and fail) -- the guard lives in Run.
	ctx := context.Background()
	d := testDB(t)
	cfg := config.Default()
	w := New(d, cfg, nil)

	err := w.snapshotOnce(ctx)
	if err == nil {
		t.Fatalf("snapshotOnce with empty Backup.Dir should fail (guard belongs in Run, not here)")
	}
}

func TestExpandHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir available")
	}
	if got, want := expandHome("~/backups"), filepath.Join(home, "backups"); got != want {
		t.Errorf("expandHome(~/backups) = %q, want %q", got, want)
	}
	if got := expandHome("/abs/path"); got != "/abs/path" {
		t.Errorf("expandHome should leave absolute paths unchanged, got %q", got)
	}
}

func TestSleepCtxRespectsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if sleepCtx(ctx, time.Hour) {
		t.Error("sleepCtx should return false immediately when ctx is already canceled")
	}
}
