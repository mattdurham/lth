// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package gwswatcher

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/mattdurham/lth/internal/config"
)

const (
	defaultPageSize  = 200
	defaultRunTimeout = 60 * time.Second
)

// Watcher polls Google Drive for matching meeting documents and writes each
// one as a markdown file into cfg.GWS.OutputDir. It does not store memories
// directly -- the markdown watcher consumes the output directory.
type Watcher struct {
	cfg    *config.Config
	runner gwsRunner
	logger *slog.Logger
}

// New constructs a Watcher. The gws binary is resolved lazily on first scan
// so the daemon can start the watcher unconditionally; if gws is missing or
// the watcher is disabled, no resolution happens and no error is raised.
func New(cfg *config.Config) *Watcher {
	return &Watcher{cfg: cfg, logger: slog.Default()}
}

// ensureRunner resolves the gws binary on demand and caches the runner on
// the Watcher. Called from ScanOnce when the watcher is enabled.
func (w *Watcher) ensureRunner() error {
	if w.runner != nil {
		return nil
	}
	r, err := newExecRunner(w.cfg.GWS.GWSBinary, defaultRunTimeout)
	if err != nil {
		return err
	}
	w.runner = r
	return nil
}

// Run is hot-reload friendly: it loops forever, checking cfg.GWS.Enabled
// on each iteration. When disabled, it sleeps for 60s and re-checks; when
// enabled, it runs ScanOnce, then sleeps for cfg.GWS.IntervalH hours.
// Toggling Enabled at runtime (via config hot-reload) takes effect on the
// next poll without a daemon restart. Returns only on ctx cancellation.
func (w *Watcher) Run(ctx context.Context) {
	const disabledPoll = 60 * time.Second
	loggedNoBinary := false
	for {
		if !w.cfg.GWS.Enabled {
			if !sleepCtx(ctx, disabledPoll) {
				return
			}
			continue
		}
		if err := w.ensureRunner(); err != nil {
			if !loggedNoBinary {
				w.logger.Warn("gwswatcher: gws binary not available", "err", err)
				loggedNoBinary = true
			}
			if !sleepCtx(ctx, disabledPoll) {
				return
			}
			continue
		}
		if err := w.ScanOnce(ctx); err != nil {
			w.logger.Warn("gwswatcher scan", "err", err)
		}
		interval := time.Duration(w.cfg.GWS.IntervalH) * time.Hour
		if interval <= 0 {
			interval = 3 * time.Hour
		}
		if !sleepCtx(ctx, interval) {
			return
		}
	}
}

// sleepCtx blocks for d or returns false if ctx is cancelled.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}

// ScanOnce performs one sweep: list matching docs, then fetch and write any
// that are missing or stale on disk.
func (w *Watcher) ScanOnce(ctx context.Context) error {
	if w.logger == nil {
		w.logger = slog.Default()
	}
	outDir := expandHome(w.cfg.GWS.OutputDir)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	since := time.Now().Add(-time.Duration(w.cfg.GWS.LookbackDays) * 24 * time.Hour)
	files, err := listMatching(ctx, w.runner,
		w.cfg.GWS.NamePatterns, w.cfg.GWS.ExcludePatterns,
		since, defaultPageSize)
	if err != nil {
		return fmt.Errorf("list drive: %w", err)
	}
	w.logger.Info("gwswatcher: scan", "candidates", len(files), "lookback_days", w.cfg.GWS.LookbackDays)

	wrote := 0
	skipped := 0
	failed := 0
	for _, f := range files {
		if err := ctx.Err(); err != nil {
			return err
		}
		out := docOutputPath(outDir, f)
		if upToDate(out, f) {
			skipped++
			continue
		}
		doc, err := fetchDoc(ctx, w.runner, f.ID)
		if err != nil {
			w.logger.Warn("gwswatcher: fetch failed", "doc", f.Name, "id", f.ID, "err", err)
			failed++
			continue
		}
		md := buildMarkdown(f, doc)
		if err := os.WriteFile(out, []byte(md), 0o600); err != nil {
			w.logger.Warn("gwswatcher: write failed", "path", out, "err", err)
			failed++
			continue
		}
		// Stamp mtime to match Drive's modifiedTime so upToDate works next cycle.
		if mt, parseErr := time.Parse(time.RFC3339, f.ModifiedTime); parseErr == nil {
			_ = os.Chtimes(out, mt, mt)
		}
		wrote++
	}
	w.logger.Info("gwswatcher: cycle complete", "wrote", wrote, "skipped", skipped, "failed", failed)
	return nil
}

// upToDate reports whether the on-disk file already mirrors driveFile's
// modifiedTime (the file exists and its mtime is >= the Drive timestamp).
func upToDate(path string, f driveFile) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	driveMtime, err := time.Parse(time.RFC3339, f.ModifiedTime)
	if err != nil {
		return false
	}
	return !info.ModTime().Before(driveMtime)
}

// docOutputPath builds a stable on-disk filename for a Drive doc. It uses the
// doc ID as the unique key (one file per doc, overwritten on update) and
// embeds a slug of the doc title for human readability.
//
//	2026-06-17_notes-by-gemini-the-ai-guild__1t0-ye11s7BPK01us-pkYOwRfkOaqyir3xqSSFtKsk-A.md
func docOutputPath(dir string, f driveFile) string {
	date := f.ModifiedTime
	if len(date) >= 10 {
		date = date[:10]
	}
	return filepath.Join(dir, date+"_"+slug(f.Name)+"__"+f.ID+".md")
}

var slugSafe = regexp.MustCompile(`[^a-z0-9]+`)

func slug(name string) string {
	s := strings.ToLower(name)
	s = slugSafe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > 80 {
		s = s[:80]
	}
	return s
}

// buildMarkdown wraps a Docs body in a small YAML-style header so the
// markdown watcher (and any human reading the file) sees provenance.
func buildMarkdown(f driveFile, doc *docResponse) string {
	var sb strings.Builder
	title := doc.Title
	if title == "" {
		title = f.Name
	}
	sb.WriteString("# ")
	sb.WriteString(title)
	sb.WriteString("\n\n")
	sb.WriteString("Source: google-workspace\n")
	sb.WriteString("Doc ID: " + f.ID + "\n")
	sb.WriteString("Modified: " + f.ModifiedTime + "\n")
	if f.WebViewLink != "" {
		sb.WriteString("Link: " + f.WebViewLink + "\n")
	}
	sb.WriteString("\n---\n\n")
	sb.WriteString(docToMarkdown(doc))
	return sb.String()
}

// expandHome replaces a leading ~/ with the user's home dir.
func expandHome(p string) string {
	if !strings.HasPrefix(p, "~/") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	return filepath.Join(home, p[2:])
}
