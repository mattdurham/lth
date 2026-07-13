// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package backupwatcher

import (
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// SnapshotInfo describes one on-disk backup snapshot.
type SnapshotInfo struct {
	Name string
	Path string
	Time time.Time
	Size int64
}

// ListSnapshots returns the snapshots in dir, oldest first.
func ListSnapshots(dir string) ([]SnapshotInfo, error) {
	matches, err := filepath.Glob(filepath.Join(dir, snapshotGlobPattern))
	if err != nil {
		return nil, fmt.Errorf("glob snapshots: %w", err)
	}
	sort.Strings(matches)

	out := make([]SnapshotInfo, 0, len(matches))
	for _, m := range matches {
		info, statErr := os.Stat(m)
		if statErr != nil {
			continue
		}
		t, _ := parseSnapshotTime(filepath.Base(m))
		out = append(out, SnapshotInfo{
			Name: filepath.Base(m),
			Path: m,
			Time: t,
			Size: info.Size(),
		})
	}
	return out, nil
}

// parseSnapshotTime extracts the timestamp from a "memory-<ts>.db.gz" filename.
func parseSnapshotTime(name string) (time.Time, error) {
	trimmed := strings.TrimSuffix(strings.TrimPrefix(name, snapshotFilePrefix), snapshotFileSuffix)
	return time.Parse(snapshotTimeFormat, trimmed)
}

// Restore decompresses snapshotPath into dbPath. If dbPath (or its -wal/-shm
// sidecars) already exist, each is copied to "<path>.pre-restore" first, so
// a restore-to-the-wrong-snapshot mistake is itself recoverable; the
// returned preRestorePath is dbPath+".pre-restore" if a copy was made, or
// "" if dbPath did not exist yet. After a successful restore, any leftover
// -wal/-shm sidecars from the database just replaced are removed: a VACUUM
// INTO snapshot is a single, self-contained file with no WAL of its own, and
// a stale sidecar belongs to a different set of pages than the restored file.
func Restore(dbPath, snapshotPath string) (preRestorePath string, err error) {
	if _, statErr := os.Stat(dbPath); statErr == nil {
		for _, suffix := range []string{"", "-wal", "-shm"} {
			src := dbPath + suffix
			if _, sErr := os.Stat(src); sErr != nil {
				continue
			}
			if cpErr := copyFile(src, src+".pre-restore"); cpErr != nil {
				return "", fmt.Errorf("pre-restore backup of %s: %w", src, cpErr)
			}
		}
		preRestorePath = dbPath + ".pre-restore"
	}

	tmpDst := dbPath + ".restoring"
	if err := gunzipFile(snapshotPath, tmpDst); err != nil {
		os.Remove(tmpDst) //nolint:errcheck
		return preRestorePath, fmt.Errorf("decompress snapshot: %w", err)
	}
	if err := os.Rename(tmpDst, dbPath); err != nil {
		os.Remove(tmpDst) //nolint:errcheck
		return preRestorePath, fmt.Errorf("finalize restore: %w", err)
	}

	os.Remove(dbPath + "-wal") //nolint:errcheck
	os.Remove(dbPath + "-shm") //nolint:errcheck

	return preRestorePath, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close() //nolint:errcheck

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close() //nolint:errcheck
		return err
	}
	return out.Close()
}

func gunzipFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open snapshot: %w", err)
	}
	defer in.Close() //nolint:errcheck

	gz, err := gzip.NewReader(in)
	if err != nil {
		return fmt.Errorf("open gzip reader: %w", err)
	}
	defer gz.Close() //nolint:errcheck

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create dest: %w", err)
	}
	if _, err := io.Copy(out, gz); err != nil { //nolint:gosec // G110: src is either lth's own snapshot or a file the user explicitly picked for `lth backup restore`, not untrusted network input
		out.Close() //nolint:errcheck
		return fmt.Errorf("decompress: %w", err)
	}
	return out.Close()
}
