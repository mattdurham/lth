// Package mdwatcher watches directories for markdown files and ingests them
// as memories using an LLM to extract all meaningful facts.
package mdwatcher

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"os/exec"

	"github.com/mattdurham/lth/internal/config"
	"github.com/mattdurham/lth/internal/gitproject"
	"github.com/mattdurham/lth/internal/llm"
	"github.com/mattdurham/lth/internal/memory"
)

const maxFileSizeBytes = 600_000 // ~150K tokens, safe for any model

// fileState tracks ingested memories for a single file.
type fileState struct {
	Hash      string   `json:"hash"`
	MemoryIDs []string `json:"memory_ids"`
}

// state is the persisted map of filepath → fileState.
type state struct {
	Files map[string]fileState `json:"files"`
}

// MDWatcher scans configured dirs for markdown files and ingests them.
type MDWatcher struct {
	store       *memory.MemoryStore
	llm         llm.LLM
	cfg         *config.Config
	stateFile   string
	mu          sync.Mutex
	st          state
	lastPull    map[string]time.Time // dir → last git pull time
}

// New creates an MDWatcher. stateFile is where ingestion state is persisted.
func New(store *memory.MemoryStore, l llm.LLM, cfg *config.Config) *MDWatcher {
	home, _ := os.UserHomeDir()
	stateFile := filepath.Join(home, ".lth", "mdwatcher-state.json")
	return &MDWatcher{
		store:     store,
		llm:       l,
		cfg:       cfg,
		stateFile: stateFile,
		lastPull:  map[string]time.Time{},
	}
}

// Run scans on startup and then on a ticker until ctx is cancelled.
func (w *MDWatcher) Run(ctx context.Context) {
	if len(w.cfg.Markdown.Dirs) == 0 {
		return
	}
	w.loadState()
	if err := w.ScanOnce(ctx); err != nil {
		slog.Error("mdwatcher initial scan", "err", err)
	}
	interval := time.Duration(w.cfg.Markdown.IntervalS) * time.Second
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := w.ScanOnce(ctx); err != nil {
				slog.Error("mdwatcher scan", "err", err)
			}
		}
	}
}

// ScanOnce performs a single scan of all configured markdown directories.
func (w *MDWatcher) ScanOnce(ctx context.Context) error {
	found := map[string]struct{}{}

	for _, dir := range w.cfg.Markdown.Dirs {
		dir = expandHome(dir)
		w.maybeGitPull(dir)
		_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			if strings.ToLower(filepath.Ext(path)) != ".md" {
				return nil
			}
			found[path] = struct{}{}
			if err := w.processFile(ctx, path); err != nil {
				slog.Warn("mdwatcher: file error", "path", path, "err", err)
			}
			return nil
		})
	}

	// Soft-delete memories for files that no longer exist.
	w.mu.Lock()
	defer w.mu.Unlock()
	for path, fs := range w.st.Files {
		if _, ok := found[path]; !ok {
			slog.Info("mdwatcher: file removed, invalidating memories", "path", path, "count", len(fs.MemoryIDs))
			w.softDeleteIDs(ctx, fs.MemoryIDs)
			delete(w.st.Files, path)
		}
	}
	w.saveState()
	return nil
}

func (w *MDWatcher) processFile(ctx context.Context, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}

	hash := fileHash(data)

	w.mu.Lock()
	prev, exists := w.st.Files[path]
	w.mu.Unlock()

	if exists && prev.Hash == hash {
		return nil // unchanged
	}

	slog.Info("mdwatcher: ingesting file", "path", path, "changed", exists)

	// Soft-delete old memories if file changed.
	if exists {
		w.softDeleteIDs(ctx, prev.MemoryIDs)
	}

	content := string(data)
	var allIDs []string

	// Chunk large files by top-level heading.
	if len(data) > maxFileSizeBytes {
		chunks := splitByHeading(content)
		for i, chunk := range chunks {
			ids, err := w.ingestChunk(ctx, path, fmt.Sprintf("part %d/%d", i+1, len(chunks)), chunk)
			if err != nil {
				slog.Warn("mdwatcher: chunk ingest failed", "path", path, "chunk", i, "err", err)
				continue
			}
			allIDs = append(allIDs, ids...)
		}
	} else {
		allIDs, err = w.ingestChunk(ctx, path, "", content)
		if err != nil {
			return fmt.Errorf("ingest %s: %w", path, err)
		}
	}

	w.mu.Lock()
	if w.st.Files == nil {
		w.st.Files = map[string]fileState{}
	}
	w.st.Files[path] = fileState{Hash: hash, MemoryIDs: allIDs}
	w.mu.Unlock()

	slog.Info("mdwatcher: ingested", "path", path, "memories", len(allIDs))
	return nil
}

func (w *MDWatcher) ingestChunk(ctx context.Context, path, part, content string) ([]string, error) {
	prompt := buildPrompt(path, part, content)
	resp, err := w.llm.Complete(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("llm: %w", err)
	}

	facts, err := parseFacts(resp)
	if err != nil {
		return nil, fmt.Errorf("parse facts: %w", err)
	}

	attrs := map[string]string{
		"source":      "markdown",
		"source_file": path,
	}
	if part != "" {
		attrs["source_part"] = part
	}
	if p := gitproject.Detect(filepath.Dir(path)); p != "" {
		attrs["project"] = p
	}

	ids := make([]string, 0, len(facts))
	for _, fact := range facts {
		if strings.TrimSpace(fact) == "" {
			continue
		}
		m, err := w.store.Store(ctx, w.cfg.Markdown.Layer, fact, attrs)
		if err != nil {
			slog.Warn("mdwatcher: store fact failed", "err", err)
			continue
		}
		ids = append(ids, m.ID)
	}
	return ids, nil
}

func (w *MDWatcher) softDeleteIDs(ctx context.Context, ids []string) {
	if len(ids) == 0 {
		return
	}
	if err := w.store.SoftDelete(ctx, ids, "markdown file changed or removed"); err != nil {
		slog.Warn("mdwatcher: soft delete failed", "err", err)
	}
}

func buildPrompt(path, part, content string) string {
	var sb strings.Builder
	sb.WriteString("Extract ALL distinct facts, insights, decisions, rules, procedures, and knowledge from this markdown document.\n\n")
	sb.WriteString("Requirements:\n")
	sb.WriteString("- Write each item as a clear, standalone sentence or short paragraph\n")
	sb.WriteString("- Each item must be fully self-contained and understandable without the source document\n")
	sb.WriteString("- Include specific values, names, paths, commands, and technical details\n")
	sb.WriteString("- Do not summarize — capture every meaningful piece of information\n")
	sb.WriteString("- Skip boilerplate, navigation, and purely structural content\n\n")
	sb.WriteString("Return ONLY a JSON array of strings, one string per memory. No other text.\n\n")
	sb.WriteString(fmt.Sprintf("Source: %s", path))
	if part != "" {
		sb.WriteString(fmt.Sprintf(" (%s)", part))
	}
	sb.WriteString("\n\nDocument:\n")
	sb.WriteString(content)
	return sb.String()
}

func parseFacts(resp string) ([]string, error) {
	resp = strings.TrimSpace(resp)
	// Strip markdown code fences if present.
	resp = strings.TrimPrefix(resp, "```json")
	resp = strings.TrimPrefix(resp, "```")
	resp = strings.TrimSuffix(resp, "```")
	resp = strings.TrimSpace(resp)

	var facts []string
	if err := json.Unmarshal([]byte(resp), &facts); err != nil {
		return nil, fmt.Errorf("unmarshal: %w (response: %.200s)", err, resp)
	}
	return facts, nil
}

func splitByHeading(content string) []string {
	var chunks []string
	var current strings.Builder
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "# ") && current.Len() > 0 {
			chunks = append(chunks, current.String())
			current.Reset()
		}
		current.WriteString(line)
		current.WriteByte('\n')
	}
	if current.Len() > 0 {
		chunks = append(chunks, current.String())
	}
	return chunks
}

func fileHash(data []byte) string {
	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h)
}

func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, path[2:])
	}
	return path
}

func (w *MDWatcher) loadState() {
	data, err := os.ReadFile(w.stateFile)
	if err != nil {
		w.st = state{Files: map[string]fileState{}}
		return
	}
	if err := json.Unmarshal(data, &w.st); err != nil {
		w.st = state{Files: map[string]fileState{}}
	}
}

func (w *MDWatcher) saveState() {
	data, err := json.Marshal(&w.st)
	if err != nil {
		return
	}
	_ = os.WriteFile(w.stateFile, data, 0o600)
}

// maybeGitPull runs `git pull` in dir if it is a git repo and enough time has
// elapsed since the last pull (GitPullIntervalS, default 3600s).
// Skips silently if git_pull is false or the dir is not a git repo.
func (w *MDWatcher) maybeGitPull(dir string) {
	if !w.cfg.Markdown.GitPull {
		return
	}
	// Only pull if it's actually a git repo.
	if gitproject.Detect(dir) == "" {
		return
	}
	interval := time.Duration(w.cfg.Markdown.GitPullIntervalS) * time.Second
	w.mu.Lock()
	last := w.lastPull[dir]
	w.mu.Unlock()
	if time.Since(last) < interval {
		return
	}
	cmd := exec.Command("git", "-C", dir, "pull", "--ff-only", "--quiet")
	out, err := cmd.CombinedOutput()
	if err != nil {
		slog.Warn("mdwatcher: git pull failed", "dir", dir, "err", err, "output", strings.TrimSpace(string(out)))
	} else {
		slog.Info("mdwatcher: git pull", "dir", dir, "output", strings.TrimSpace(string(out)))
	}
	w.mu.Lock()
	w.lastPull[dir] = time.Now()
	w.mu.Unlock()
}
