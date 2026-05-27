// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// Package watcher provides fsnotify-based JSONL file watching and ingestion as L5 memories.
package watcher

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/fsnotify/fsnotify"
	"github.com/mattdurham/lth/internal/config"
	"github.com/mattdurham/lth/internal/memory"
)

// Watcher watches JSONL files and ingests new messages as L5 memories.
type Watcher struct {
	store   memory.Store
	cfg     *config.Config
	watcher *fsnotify.Watcher
	offsets map[string]int64
	mu      sync.Mutex
	logger  *slog.Logger
}

// New creates a new Watcher. Call Start to begin watching.
func New(store memory.Store, cfg *config.Config) (*Watcher, error) {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("create fsnotify watcher: %w", err)
	}

	w := &Watcher{
		store:   store,
		cfg:     cfg,
		watcher: fw,
		offsets: make(map[string]int64),
		logger:  slog.Default(),
	}

	if err := w.loadOffsets(); err != nil {
		// Non-fatal: start fresh if state file is missing or corrupt.
		w.logger.Warn("could not load watcher offsets", "err", err)
	}

	return w, nil
}

// Start begins watching configured paths for JSONL changes. Blocking; stops on ctx cancel.
func (w *Watcher) Start(ctx context.Context) error {
	defer w.watcher.Close() //nolint:errcheck

	// Add watch paths.
	for _, watchPath := range w.cfg.Watcher.Paths {
		expanded := expandHome(watchPath)
		if err := addPathRecursive(w.watcher, expanded); err != nil {
			w.logger.Warn("could not watch path", "path", expanded, "err", err)
		}
	}

	// Initial scan of all existing JSONL files.
	for _, watchPath := range w.cfg.Watcher.Paths {
		expanded := expandHome(watchPath)
		w.scanExisting(ctx, expanded)
	}

	// Event loop.
	for {
		select {
		case <-ctx.Done():
			return nil
		case event, ok := <-w.watcher.Events:
			if !ok {
				return nil
			}
			if event.Op&(fsnotify.Write|fsnotify.Create) != 0 {
				if strings.HasSuffix(event.Name, ".jsonl") {
					if err := w.ingestFile(ctx, event.Name); err != nil {
						w.logger.Warn("ingest error", "file", event.Name, "err", err)
					}
				}
			}
		case err, ok := <-w.watcher.Errors:
			if !ok {
				return nil
			}
			w.logger.Warn("watcher error", "err", err)
		}
	}
}

// ingestFile reads new bytes from path (from stored offset to EOF) and stores messages.
func (w *Watcher) ingestFile(ctx context.Context, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open file: %w", err)
	}
	defer f.Close() //nolint:errcheck

	w.mu.Lock()
	offset := w.offsets[path]
	w.mu.Unlock()

	if _, err := f.Seek(offset, 0); err != nil {
		return fmt.Errorf("seek to offset %d: %w", offset, err)
	}

	format := detectFormat(path)

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // 1MB line buffer

	// sessionPaths accumulates touched file paths per session for this ingest batch.
	// Only used for Claude-format files.
	sessionPaths := make(map[string]map[string]struct{})

	// carriedCWD holds the cwd from the wllr session header line and is applied
	// to all subsequent message lines in the same file. A second session header
	// mid-file updates carriedCWD for all subsequent messages.
	var carriedCWD string

	for scanner.Scan() {
		line := scanner.Bytes()

		var content, sessionID, cwd string
		var skip bool

		if format == FormatWllr {
			// ParseWllrLine returns the session cwd only for session-type lines (skip=true).
			// For message lines it returns empty cwd and skip=false; we use carriedCWD directly.
			var parseErr error
			var sessionCWD string
			content, sessionID, sessionCWD, _, skip, parseErr = ParseWllrLine(line, carriedCWD)
			if parseErr != nil {
				w.logger.Warn("wllr parse error", "file", path, "err", parseErr)
				continue
			}
			if sessionCWD != "" {
				carriedCWD = sessionCWD
			}
			// Use carriedCWD for the memory attrs; ParseWllrLine returns empty cwd for
			// message lines — the cwd lives on the session header and is carried forward here.
			cwd = carriedCWD
		} else {
			// Claude format: collect file paths from tool_use blocks independently.
			if filePaths, sid := ParseFilePaths(line); len(filePaths) > 0 {
				if sessionPaths[sid] == nil {
					sessionPaths[sid] = make(map[string]struct{})
				}
				for _, fp := range filePaths {
					sessionPaths[sid][fp] = struct{}{}
				}
			}
			var parseErr error
			content, sessionID, cwd, _, skip, parseErr = ParseLine(line)
			if parseErr != nil {
				continue
			}
		}

		if skip || content == "" {
			continue
		}

		attrs := map[string]string{
			"source":  "watcher",
			"session": sessionID,
			"cwd":     cwd,
			"file":    path,
		}
		if _, err := w.store.Store(ctx, 5, content, attrs); err != nil {
			w.logger.Warn("store memory error", "err", err)
		}
	}

	// Store one compact "files touched" memory per session seen in this batch (Claude only).
	for sid, paths := range sessionPaths {
		w.storeFilesTouched(ctx, sid, paths)
	}

	// Update offset to current position.
	pos, err := f.Seek(0, 1) // current position
	if err == nil {
		w.mu.Lock()
		w.offsets[path] = pos
		w.mu.Unlock()
		_ = w.saveOffsets()
	}

	return scanner.Err()
}

// storeFilesTouched stores a compact L5 memory listing the files touched in a session batch.
func (w *Watcher) storeFilesTouched(ctx context.Context, sessionID string, paths map[string]struct{}) {
	if len(paths) == 0 {
		return
	}

	sorted := make([]string, 0, len(paths))
	for p := range paths {
		sorted = append(sorted, p)
	}
	sort.Strings(sorted)

	// Detect repo module from the first absolute path that resolves.
	repo := ""
	for _, p := range sorted {
		if filepath.IsAbs(p) {
			if r := RepoForPath(p); r != "" {
				repo = r
				break
			}
		}
	}

	var sb strings.Builder
	sb.WriteString("Files touched:\n")
	for _, p := range sorted {
		sb.WriteString("  ")
		sb.WriteString(p)
		sb.WriteString("\n")
	}

	attrs := map[string]string{
		"source":  "watcher",
		"session": sessionID,
		"repo":    repo,
	}
	if _, err := w.store.Store(ctx, 5, sb.String(), attrs); err != nil {
		w.logger.Warn("store files-touched error", "err", err)
	}
}

// scanExisting ingests all existing JSONL files from their stored offsets.
func (w *Watcher) scanExisting(ctx context.Context, dir string) {
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil //nolint:nilerr // Walk: skip inaccessible paths and non-JSONL files intentionally
		}
		if ingestErr := w.ingestFile(ctx, path); ingestErr != nil {
			w.logger.Warn("initial ingest error", "file", path, "err", ingestErr)
		}
		return nil
	})
}

// loadOffsets reads the watcher state file.
func (w *Watcher) loadOffsets() error {
	path := expandHome(w.cfg.Watcher.StateFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	var state struct {
		Offsets map[string]int64 `json:"offsets"`
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return fmt.Errorf("parse offsets: %w", err)
	}

	w.mu.Lock()
	w.offsets = state.Offsets
	if w.offsets == nil {
		w.offsets = make(map[string]int64)
	}
	w.mu.Unlock()
	return nil
}

// saveOffsets writes the watcher state file atomically using a unique temp file.
func (w *Watcher) saveOffsets() error {
	path := expandHome(w.cfg.Watcher.StateFile)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}

	w.mu.Lock()
	state := struct {
		Offsets map[string]int64 `json:"offsets"`
	}{Offsets: w.offsets}
	w.mu.Unlock()

	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("marshal offsets: %w", err)
	}

	tmp, err := os.CreateTemp(dir, "watcher-state-*.json")
	if err != nil {
		return fmt.Errorf("create temp offsets file: %w", err)
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()           //nolint:errcheck
		os.Remove(tmpName)    //nolint:errcheck
		return fmt.Errorf("write tmp offsets: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName) //nolint:errcheck
		return fmt.Errorf("close tmp offsets: %w", err)
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		os.Remove(tmpName) //nolint:errcheck
		return fmt.Errorf("chmod tmp offsets: %w", err)
	}
	return os.Rename(tmpName, path)
}

// addPathRecursive adds a directory tree to the fsnotify watcher.
func addPathRecursive(fw *fsnotify.Watcher, dir string) error {
	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil //nolint:nilerr // Walk: skip inaccessible paths intentionally
		}
		if info.IsDir() {
			return fw.Add(path)
		}
		return nil
	})
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
