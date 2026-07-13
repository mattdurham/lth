// Package backupwatcher takes daily VACUUM INTO snapshots of the memory
// database, gzips them into a user-configured directory, and prunes all but
// the most recent N -- a self-contained disaster-recovery mechanism, not a
// substitute for lth export/import (which optimizes for portability, not
// fast, drop-in restoration).
//
// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.
package backupwatcher

import (
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mattdurham/lth/internal/config"
	"github.com/mattdurham/lth/internal/db"
	"github.com/mattdurham/lth/internal/metrics"
)

// snapshotTimeFormat is used for both the on-disk filename and parsing it
// back in ListSnapshots. Fixed-width and zero-padded, so lexicographic
// filename sort equals chronological order.
const snapshotTimeFormat = "20060102-150405"

const (
	tmpDBPrefix   = ".tmp-memory-"
	partialSuffix = ".part"

	// snapshotFilePrefix and snapshotFileSuffix compose every finished
	// snapshot's filename ("memory-<ts>.db.gz"); snapshotGlobPattern is the
	// same shape as a filepath.Glob pattern. Centralized so the finalize,
	// list, prune, and stale-cleanup call sites (here and in restore.go)
	// can't drift out of sync with each other.
	snapshotFilePrefix  = "memory-"
	snapshotFileSuffix  = ".db.gz"
	snapshotGlobPattern = snapshotFilePrefix + "*" + snapshotFileSuffix
)

// Watcher periodically snapshots the database into Backup.Dir.
type Watcher struct {
	d       *db.DB
	cfg     *config.Config
	metrics *metrics.Metrics
}

// New creates a backup Watcher.
func New(d *db.DB, cfg *config.Config, m *metrics.Metrics) *Watcher {
	return &Watcher{d: d, cfg: cfg, metrics: m}
}

// Run is hot-reload friendly: it loops forever, checking cfg.Backup.Dir on
// each iteration. When empty, it sleeps for 60s and re-checks; Dir set via
// config hot-reload is picked up on the next tick without requiring a
// daemon restart. Returns only on ctx cancellation.
func (w *Watcher) Run(ctx context.Context) {
	const disabledPoll = 60 * time.Second
	for {
		if w.cfg.Backup.Dir == "" {
			if !sleepCtx(ctx, disabledPoll) {
				return
			}
			continue
		}
		if err := w.snapshotOnce(ctx); err != nil {
			slog.Warn("backupwatcher: snapshot failed", "err", err)
		}
		interval := time.Duration(w.cfg.Backup.IntervalH) * time.Hour
		if interval <= 0 {
			interval = time.Duration(config.DefaultBackupIntervalH) * time.Hour
		}
		if !sleepCtx(ctx, interval) {
			return
		}
	}
}

// sleepCtx blocks for d or returns false if ctx is canceled.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}

// snapshotOnce takes one VACUUM INTO snapshot, compresses it, and prunes old
// snapshots. A failure at any step is returned to the caller (who logs and
// retries next tick) without touching retention -- pruning only runs after a
// successful snapshot, so one bad tick can never cascade into losing older,
// still-good backups.
func (w *Watcher) snapshotOnce(ctx context.Context) error {
	dir := expandHome(w.cfg.Backup.Dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		w.recordFailure()
		return fmt.Errorf("create backup dir: %w", err)
	}
	cleanStale(dir)

	ts := time.Now().UTC().Format(snapshotTimeFormat)
	tmpDBPath := filepath.Join(dir, tmpDBPrefix+ts+".db")
	finalPath := filepath.Join(dir, snapshotFilePrefix+ts+snapshotFileSuffix)
	partialPath := finalPath + partialSuffix

	if err := w.d.VacuumInto(ctx, tmpDBPath); err != nil {
		w.recordFailure()
		return fmt.Errorf("vacuum into: %w", err)
	}
	defer os.Remove(tmpDBPath) //nolint:errcheck

	if err := gzipFile(tmpDBPath, partialPath); err != nil {
		os.Remove(partialPath) //nolint:errcheck
		w.recordFailure()
		return fmt.Errorf("compress snapshot: %w", err)
	}
	if err := os.Rename(partialPath, finalPath); err != nil {
		os.Remove(partialPath) //nolint:errcheck
		w.recordFailure()
		return fmt.Errorf("finalize snapshot: %w", err)
	}

	w.recordSuccess(fileSize(finalPath))

	keep := w.cfg.Backup.Keep
	if keep <= 0 {
		keep = config.DefaultBackupKeep
	}
	if err := pruneOldSnapshots(dir, keep); err != nil {
		slog.Warn("backupwatcher: prune failed", "dir", dir, "err", err)
	}
	return nil
}

// cleanStale removes temp/partial files left behind by a snapshot attempt
// that crashed mid-way (mid-VACUUM or mid-gzip). Best-effort; failures are
// logged, not fatal.
func cleanStale(dir string) {
	for _, pattern := range []string{tmpDBPrefix + "*.db", snapshotGlobPattern + partialSuffix} {
		matches, _ := filepath.Glob(filepath.Join(dir, pattern))
		for _, m := range matches {
			if err := os.Remove(m); err != nil {
				slog.Warn("backupwatcher: cleanup stale file failed", "path", m, "err", err)
			} else {
				slog.Info("backupwatcher: removed stale file from a previous run", "path", m)
			}
		}
	}
}

// gzipFile compresses src into dst. dst must not already exist (O_EXCL) --
// cleanStale already clears any leftover partial from a prior crash, so a
// collision here indicates something unexpected and should fail loudly
// rather than silently overwrite.
func gzipFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer in.Close() //nolint:errcheck

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create dest: %w", err)
	}

	gz := gzip.NewWriter(out)
	if _, copyErr := io.Copy(gz, in); copyErr != nil {
		gz.Close()  //nolint:errcheck
		out.Close() //nolint:errcheck
		return fmt.Errorf("compress: %w", copyErr)
	}
	if err := gz.Close(); err != nil {
		out.Close() //nolint:errcheck
		return fmt.Errorf("finalize gzip: %w", err)
	}
	return out.Close()
}

// fileSize returns path's size in bytes, or 0 if it cannot be stat'd.
func fileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}

// pruneOldSnapshots keeps the keep most recent memory-*.db.gz files in dir
// (by filename sort, which is chronological) and removes the rest.
func pruneOldSnapshots(dir string, keep int) error {
	matches, err := filepath.Glob(filepath.Join(dir, snapshotGlobPattern))
	if err != nil {
		return fmt.Errorf("glob snapshots: %w", err)
	}
	sort.Strings(matches)
	if len(matches) <= keep {
		return nil
	}
	var firstErr error
	for _, m := range matches[:len(matches)-keep] {
		if err := os.Remove(m); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("remove %s: %w", m, err)
		}
	}
	return firstErr
}

func (w *Watcher) recordFailure() {
	if w.metrics != nil {
		w.metrics.BackupSnapshotsTotal.WithLabelValues("failure").Inc()
	}
}

func (w *Watcher) recordSuccess(size int64) {
	if w.metrics != nil {
		w.metrics.BackupSnapshotsTotal.WithLabelValues("success").Inc()
		w.metrics.BackupLastSuccessTimestamp.SetToCurrentTime()
		w.metrics.BackupSnapshotBytes.Set(float64(size))
	}
}

// expandHome replaces a leading ~ with the user's home directory.
func expandHome(path string) string {
	if !strings.HasPrefix(path, "~/") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, path[2:])
}
