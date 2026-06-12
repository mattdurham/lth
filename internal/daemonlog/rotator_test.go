// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package daemonlog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNew_CreatesFresh(t *testing.T) {
	dir := t.TempDir()
	r, err := New(Options{Path: filepath.Join(dir, "daemon.log")})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close() //nolint:errcheck
	if _, err := os.Stat(filepath.Join(dir, "daemon.log")); err != nil {
		t.Errorf("daemon.log not created: %v", err)
	}
}

func TestWrite_AppendsAndFsync(t *testing.T) {
	dir := t.TempDir()
	r, _ := New(Options{Path: filepath.Join(dir, "daemon.log")})
	defer r.Close() //nolint:errcheck
	if _, err := r.Write([]byte("hello\n")); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "daemon.log"))
	if string(data) != "hello\n" {
		t.Errorf("file contents = %q", data)
	}
}

func TestArchiveStaleOnDisk(t *testing.T) {
	dir := t.TempDir()
	cur := filepath.Join(dir, "daemon.log")
	if err := os.WriteFile(cur, []byte("old-content\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Backdate mtime by 2 days.
	twoDaysAgo := time.Now().AddDate(0, 0, -2)
	if err := os.Chtimes(cur, twoDaysAgo, twoDaysAgo); err != nil {
		t.Fatal(err)
	}

	r, err := New(Options{Path: cur})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close() //nolint:errcheck

	// Expect daemon-<2daysAgo>.log to exist with the old content.
	archived := filepath.Join(dir, "daemon-"+twoDaysAgo.UTC().Format(dateLayout)+".log")
	if _, err := os.Stat(archived); err != nil {
		t.Errorf("archived file missing: %v", err)
	}
	data, _ := os.ReadFile(archived)
	if string(data) != "old-content\n" {
		t.Errorf("archived content = %q", data)
	}
	// Current daemon.log should be empty (freshly opened).
	info, _ := os.Stat(cur)
	if info.Size() != 0 {
		t.Errorf("current log not fresh: size=%d", info.Size())
	}
}

func TestRotation_ManualTrigger(t *testing.T) {
	dir := t.TempDir()
	r, _ := New(Options{Path: filepath.Join(dir, "daemon.log")})
	defer r.Close() //nolint:errcheck

	_, _ = r.Write([]byte("today-line\n"))

	// Simulate day rollover by mutating curDate.
	r.mu.Lock()
	r.curDate = "2020-01-01"
	r.mu.Unlock()

	_, _ = r.Write([]byte("post-rollover\n"))

	archived := filepath.Join(dir, "daemon-2020-01-01.log")
	if _, err := os.Stat(archived); err != nil {
		t.Errorf("archive not created on rotation: %v", err)
	}
	archData, _ := os.ReadFile(archived)
	if string(archData) != "today-line\n" {
		t.Errorf("archive content = %q", archData)
	}

	curData, _ := os.ReadFile(filepath.Join(dir, "daemon.log"))
	if string(curData) != "post-rollover\n" {
		t.Errorf("current content = %q", curData)
	}
}

func TestPurgeOld_RemovesBeyondRetention(t *testing.T) {
	dir := t.TempDir()
	// Pre-create several archive files at various dates.
	today := time.Now().UTC()
	for _, daysAgo := range []int{1, 2, 3, 4, 5, 10} {
		date := today.AddDate(0, 0, -daysAgo).Format(dateLayout)
		_ = os.WriteFile(filepath.Join(dir, "daemon-"+date+".log"), []byte("x"), 0o600)
	}

	r, _ := New(Options{Path: filepath.Join(dir, "daemon.log"), RetainDays: 3})
	defer r.Close() //nolint:errcheck

	entries, _ := os.ReadDir(dir)
	var archives []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "daemon-") {
			archives = append(archives, e.Name())
		}
	}
	// Cutoff is today - 3 days. We keep daysAgo = 1, 2, and any with date >= cutoff.
	// daysAgo=3 has date == cutoff, which is NOT strictly less than cutoff, so kept.
	// daysAgo=4, 5, 10 are below cutoff -> purged.
	if len(archives) != 3 {
		t.Errorf("expected 3 archives (1,2,3 days ago), got %d: %v", len(archives), archives)
	}
}

func TestPurgeOld_DisabledWhenRetainDaysZero(t *testing.T) {
	dir := t.TempDir()
	old := time.Now().AddDate(0, 0, -30).UTC().Format(dateLayout)
	_ = os.WriteFile(filepath.Join(dir, "daemon-"+old+".log"), []byte("x"), 0o600)

	r, _ := New(Options{Path: filepath.Join(dir, "daemon.log"), RetainDays: 0})
	defer r.Close() //nolint:errcheck

	if _, err := os.Stat(filepath.Join(dir, "daemon-"+old+".log")); err != nil {
		t.Errorf("RetainDays=0 should not purge; file gone: %v", err)
	}
}

func TestPurgeOld_IgnoresMalformedNames(t *testing.T) {
	dir := t.TempDir()
	// Ill-formed filenames should be left alone.
	for _, name := range []string{"daemon-notadate.log", "daemon-2020-13-99.log", "unrelated.log"} {
		_ = os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600)
	}
	r, _ := New(Options{Path: filepath.Join(dir, "daemon.log"), RetainDays: 1})
	defer r.Close() //nolint:errcheck

	for _, name := range []string{"daemon-notadate.log", "daemon-2020-13-99.log", "unrelated.log"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("malformed file %s incorrectly removed: %v", name, err)
		}
	}
}
