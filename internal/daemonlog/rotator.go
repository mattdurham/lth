// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// Package daemonlog provides a daily-rotating log writer for the lth daemon.
//
// Files are named:
//
//	daemon.log                       current day, always written to
//	daemon-YYYY-MM-DD.log            archived previous days
//
// Rotation is lazy: the next Write after midnight (UTC) closes the current
// file, renames it to daemon-YYYY-MM-DD.log using the date of its last write,
// opens a fresh daemon.log, and prunes any archive older than RetainDays.
//
// The rotator is safe for concurrent use. On rotation it also calls dup2 to
// point os.Stdout and os.Stderr at the new file, so panics and any third-party
// code that writes directly to those fds still land in the current log.
package daemonlog

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

const dateLayout = "2006-01-02"

// Rotator is an io.WriteCloser that writes to a daily-rotating log file.
type Rotator struct {
	dir         string
	baseName    string // e.g. "daemon" (no extension)
	retainDays  int    // delete archives older than this many days; <=0 disables pruning
	redirectFDs bool   // dup2 os.Stdout/os.Stderr to the current file on every open

	mu       sync.Mutex
	f        *os.File
	curDate  string // YYYY-MM-DD of the day in which f was opened
}

// Options configures a Rotator.
type Options struct {
	// Path is the canonical current log path (e.g. "/home/u/.lth/daemon.log").
	Path string
	// RetainDays is how many archived YYYY-MM-DD files to keep. <=0 disables pruning.
	RetainDays int
	// RedirectStdFDs, if true, also redirects os.Stdout and os.Stderr to the
	// rotator's current file via dup2. Should be true for daemon use so that
	// panics and direct prints land in the same file as slog.
	RedirectStdFDs bool
}

// New constructs a Rotator and opens the current log file. If the existing
// daemon.log on disk was last written on a previous day, it is archived
// (renamed) under that day before the fresh file is opened.
func New(opts Options) (*Rotator, error) {
	dir := filepath.Dir(opts.Path)
	base := filepath.Base(opts.Path)
	if ext := filepath.Ext(base); ext != "" {
		base = strings.TrimSuffix(base, ext)
	}
	r := &Rotator{
		dir:         dir,
		baseName:    base,
		retainDays:  opts.RetainDays,
		redirectFDs: opts.RedirectStdFDs,
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir log dir: %w", err)
	}

	// If an existing daemon.log was last written on a prior day, archive it
	// before we open today's file.
	if err := r.archiveStaleOnDisk(); err != nil {
		return nil, err
	}
	if err := r.openCurrent(); err != nil {
		return nil, err
	}
	r.purgeOld()
	return r, nil
}

// Write implements io.Writer. It checks for day rollover before each write.
func (r *Rotator) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.f == nil {
		return 0, io.ErrClosedPipe
	}
	if today := time.Now().UTC().Format(dateLayout); today != r.curDate {
		if err := r.rotateLocked(); err != nil {
			return 0, err
		}
	}
	return r.f.Write(p)
}

// Close flushes and closes the current file.
func (r *Rotator) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.f == nil {
		return nil
	}
	err := r.f.Close()
	r.f = nil
	return err
}

// archiveStaleOnDisk renames daemon.log to daemon-<lastModDate>.log if its
// last-modified date is not today. No-op if the file does not exist.
func (r *Rotator) archiveStaleOnDisk() error {
	cur := r.currentPath()
	info, err := os.Stat(cur)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat current log: %w", err)
	}
	if info.Size() == 0 {
		return nil
	}
	modDate := info.ModTime().UTC().Format(dateLayout)
	if modDate == time.Now().UTC().Format(dateLayout) {
		return nil // still today; just append
	}
	archived := r.archivePath(modDate)
	if err := os.Rename(cur, archived); err != nil {
		return fmt.Errorf("archive stale log: %w", err)
	}
	return nil
}

// openCurrent opens daemon.log for append and updates curDate to today.
// On success and if redirectFDs is set, dup2 is called to point os.Stdout and
// os.Stderr at the new fd.
func (r *Rotator) openCurrent() error {
	cur := r.currentPath()
	//nolint:gosec // G304: path is built from configured log dir, not user input
	f, err := os.OpenFile(cur, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open log: %w", err)
	}
	r.f = f
	r.curDate = time.Now().UTC().Format(dateLayout)

	if r.redirectFDs {
		// dup2 the new fd over stdout and stderr so direct prints follow rotation.
		// Errors here are non-fatal; logging still works via the rotator writer.
		_ = syscall.Dup2(int(f.Fd()), int(os.Stdout.Fd()))
		_ = syscall.Dup2(int(f.Fd()), int(os.Stderr.Fd()))
	}
	return nil
}

// rotateLocked archives the current file under its open-date and opens a fresh
// daemon.log. Caller must hold r.mu.
func (r *Rotator) rotateLocked() error {
	prevDate := r.curDate
	if r.f != nil {
		_ = r.f.Close()
		r.f = nil
	}
	cur := r.currentPath()
	archived := r.archivePath(prevDate)
	if err := os.Rename(cur, archived); err != nil && !os.IsNotExist(err) {
		// Continue: tolerate weird FS state by opening fresh anyway.
	}
	if err := r.openCurrent(); err != nil {
		return err
	}
	r.purgeOld()
	return nil
}

// purgeOld removes archived daemon-YYYY-MM-DD.log files older than retainDays.
func (r *Rotator) purgeOld() {
	if r.retainDays <= 0 {
		return
	}
	cutoff := time.Now().UTC().AddDate(0, 0, -r.retainDays).Format(dateLayout)
	entries, err := os.ReadDir(r.dir)
	if err != nil {
		return
	}
	prefix := r.baseName + "-"
	const suffix = ".log"
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, suffix) {
			continue
		}
		datePart := strings.TrimSuffix(strings.TrimPrefix(name, prefix), suffix)
		// Sanity: parseable date and older than cutoff (string compare works for ISO dates).
		if _, perr := time.Parse(dateLayout, datePart); perr != nil {
			continue
		}
		if datePart < cutoff {
			_ = os.Remove(filepath.Join(r.dir, name))
		}
	}
}

func (r *Rotator) currentPath() string {
	return filepath.Join(r.dir, r.baseName+".log")
}

func (r *Rotator) archivePath(date string) string {
	return filepath.Join(r.dir, r.baseName+"-"+date+".log")
}
