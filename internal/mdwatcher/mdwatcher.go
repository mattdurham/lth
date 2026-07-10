// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

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
	"github.com/mattdurham/lth/internal/metrics"
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
	store     memory.Store
	llm       llm.LLM
	cfg       *config.Config
	stateFile string
	mu        sync.Mutex
	st        state
	lastPull  map[string]time.Time // dir → last git pull time
	metrics   *metrics.Metrics
}

// New creates an MDWatcher. stateFile is where ingestion state is persisted.
func New(store memory.Store, l llm.LLM, cfg *config.Config, m *metrics.Metrics) *MDWatcher {
	home, _ := os.UserHomeDir()
	stateFile := filepath.Join(home, ".lth", "mdwatcher-state.json")
	return &MDWatcher{
		store:     store,
		llm:       l,
		metrics:   m,
		cfg:       cfg,
		stateFile: stateFile,
		lastPull:  map[string]time.Time{},
	}
}

// Run is hot-reload friendly: it loops forever, checking cfg.Markdown.Dirs
// and cfg.Markdown.GitHub.Repos on each iteration. When both are empty, it
// sleeps for 60s and re-checks; otherwise it runs ScanOnce and sleeps for
// cfg.Markdown.IntervalS seconds. Repos and dirs added via config hot-reload
// are picked up on the next scan without requiring a daemon restart.
func (w *MDWatcher) Run(ctx context.Context) {
	const disabledPoll = 60 * time.Second
	stateLoaded := false
	for {
		if len(w.cfg.Markdown.Dirs) == 0 && len(w.cfg.Markdown.GitHub.Repos) == 0 {
			if !sleepCtx(ctx, disabledPoll) {
				return
			}
			continue
		}
		if !stateLoaded {
			w.loadState()
			stateLoaded = true
		}
		if err := w.ScanOnce(ctx); err != nil {
			slog.Error("mdwatcher scan", "err", err)
		}
		interval := time.Duration(w.cfg.Markdown.IntervalS) * time.Second
		if interval <= 0 {
			interval = 5 * time.Minute
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

// ScanOnce performs a single scan of all configured markdown directories
// and configured GitHub repos.
func (w *MDWatcher) ScanOnce(ctx context.Context) error {
	found := map[string]struct{}{}

	// Build the effective dir list: cfg.Markdown.Dirs plus, if enabled, the
	// GWS watcher's output directory. The gws-imports dir is added
	// dynamically per scan rather than persisted into Markdown.Dirs, so a
	// config hot-reload that re-reads the YAML cannot accidentally drop it
	// and trigger a wave of "file removed" soft-deletes on the gws-derived
	// memories.
	dirs := append([]string(nil), w.cfg.Markdown.Dirs...)
	if w.cfg.GWS.Enabled && w.cfg.GWS.OutputDir != "" {
		gwsDir := expandHome(w.cfg.GWS.OutputDir)
		alreadyIn := false
		for _, d := range dirs {
			if expandHome(d) == gwsDir {
				alreadyIn = true
				break
			}
		}
		if !alreadyIn {
			dirs = append(dirs, gwsDir)
		}
	}

	// Local dirs: scan the entire tree.
	for _, dir := range dirs {
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

	// GitHub repos: clone/refresh, then scan with include/exclude filtering.
	if len(w.cfg.Markdown.GitHub.Repos) > 0 {
		w.scanGitHubRepos(ctx, found)
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

	// Chunk large files using a format-aware splitter (markdown headings,
	// YAML document separators, or size-windowed lines for everything else).
	if len(data) > maxFileSizeBytes {
		chunks := splitForLLM(path, content, maxFileSizeBytes)
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
		mem, err := w.store.Store(ctx, w.cfg.Markdown.Layer, fact, attrs)
		if err != nil {
			slog.Warn("mdwatcher: store fact failed", "err", err)
			continue
		}
		ids = append(ids, mem.ID)
		if w.metrics != nil {
			w.metrics.MarkdownIngestedTotal.WithLabelValues(filepath.Dir(path)).Inc()
		}
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
	sb.WriteString("Extract ALL distinct facts, insights, decisions, rules, procedures, and knowledge from this document.\n\n")
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

// splitForLLM breaks content into LLM-sized chunks using a strategy chosen
// by file extension. After the format-aware first pass, any chunk still
// larger than maxBytes is line-windowed so no single chunk exceeds the LLM
// context budget.
//
//	.md, .markdown   split at top-level "# " headings
//	.yaml, .yml      split at YAML document separator "---"
//	anything else    size-windowed at line boundaries
//
// A single line longer than maxBytes is emitted as its own (oversized)
// chunk; the LLM call will return a context-overflow error which the chain
// surfaces normally rather than silently truncating content.
func splitForLLM(path, content string, maxBytes int) []string {
	var raw []string
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".markdown":
		raw = splitByHeading(content)
	case ".yaml", ".yml":
		raw = splitByYAMLDocs(content)
	default:
		raw = []string{content}
	}
	var out []string
	for _, c := range raw {
		if len(c) <= maxBytes {
			out = append(out, c)
			continue
		}
		out = append(out, windowByLines(c, maxBytes)...)
	}
	return out
}

// splitByHeading splits markdown content at lines beginning with "# "
// (top-level H1 headings). The heading line stays at the top of its chunk.
func splitByHeading(content string) []string {
	var chunks []string
	var current strings.Builder
	for _, line := range splitLines(content) {
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

// splitByYAMLDocs splits content at lines containing only "---", the YAML
// document separator. The separator itself is discarded. Files without a
// separator are returned as a single chunk.
func splitByYAMLDocs(content string) []string {
	var chunks []string
	var current strings.Builder
	for _, line := range splitLines(content) {
		if strings.TrimRight(line, " \t") == "---" && current.Len() > 0 {
			chunks = append(chunks, current.String())
			current.Reset()
			continue
		}
		current.WriteString(line)
		current.WriteByte('\n')
	}
	if current.Len() > 0 {
		chunks = append(chunks, current.String())
	}
	if len(chunks) == 0 {
		return []string{content}
	}
	return chunks
}

// windowByLines splits content into chunks of at most maxBytes by accumulating
// lines. A line is appended to the current chunk if it fits; otherwise the
// current chunk is flushed and the line starts a new one. A single line
// longer than maxBytes is emitted as its own oversized chunk -- callers see
// the resulting LLM error rather than silent mid-line truncation.
func windowByLines(content string, maxBytes int) []string {
	var chunks []string
	var current strings.Builder
	for _, line := range splitLines(content) {
		cost := len(line) + 1 // +1 for the trailing newline we add
		if current.Len() > 0 && current.Len()+cost > maxBytes {
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

// splitLines is like strings.Split(s, "\n") but drops the trailing empty
// element that results from a string ending in "\n". This keeps the chunking
// helpers from emitting a phantom empty line at the end of well-formed input.
func splitLines(s string) []string {
	lines := strings.Split(s, "\n")
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1]
	}
	return lines
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

// scanGitHubRepos resolves the configured GitHub repos (clone/fetch as
// needed) and walks each one, honouring per-repo include/exclude prefix
// filters. Files that pass the filter and end in .md are processed; their
// paths are recorded in `found` so the global stale-file cleanup logic
// (which soft-deletes memories for vanished files) applies to them.
//
// Throttling: EnsureRepo runs at most once per Markdown.GitPullIntervalS
// (the same knob that controls existing local-dir git pulls), so a tight
// scan ticker does not hammer GitHub.
func (w *MDWatcher) scanGitHubRepos(ctx context.Context, found map[string]struct{}) {
	w.mu.Lock()
	interval := time.Duration(w.cfg.Markdown.GitPullIntervalS) * time.Second
	gh := w.cfg.Markdown.GitHub
	// Expand any leading ~/ in the cache dir -- the YAML loader does not do
	// this, so a config like `cache_dir: ~/.lth/repos-cache` would otherwise
	// be treated as a literal relative path.
	gh.CacheDir = expandHome(gh.CacheDir)
	repos := make([]config.MarkdownGitHubRepo, 0, len(gh.Repos))
	// Build the subset due for refresh; the rest we reuse from the previous
	// scan via their well-known on-disk paths.
	dueRefresh := make(map[string]bool, len(gh.Repos))
	for _, spec := range gh.Repos {
		if time.Since(w.lastPull["github:"+spec.Repo]) >= interval {
			dueRefresh[spec.Repo] = true
		}
		repos = append(repos, spec)
	}
	w.mu.Unlock()

	for _, spec := range repos {
		var root string
		if dueRefresh[spec.Repo] {
			r, err := EnsureRepo(ctx, gh.CacheDir, gh.CloneDepth, spec)
			if err != nil {
				slog.Warn("mdwatcher: github repo sync failed", "repo", spec.Repo, "err", err)
				continue
			}
			root = r.RootDir
			w.mu.Lock()
			w.lastPull["github:"+spec.Repo] = time.Now()
			w.mu.Unlock()
		} else {
			// Reuse on-disk path; skip refresh this cycle.
			org, name, _ := strings.Cut(spec.Repo, "/")
			root = filepath.Join(gh.CacheDir, org, name)
			if _, err := os.Stat(filepath.Join(root, ".git")); err != nil {
				// Cache dir missing -- force a clone next cycle.
				w.mu.Lock()
				w.lastPull["github:"+spec.Repo] = time.Time{}
				w.mu.Unlock()
				continue
			}
		}

		_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil //nolint:nilerr // tolerate permission errors in cached clones
			}
			if d.IsDir() {
				if d.Name() == ".git" {
					return filepath.SkipDir
				}
				return nil
			}
			if !extensionMatches(path, spec.FileTypes) {
				return nil
			}
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return nil
			}
			if !pathAccepted(rel, spec.Include, spec.Exclude) {
				return nil
			}
			found[path] = struct{}{}
			if err := w.processFile(ctx, path); err != nil {
				slog.Warn("mdwatcher: file error", "path", path, "err", err)
			}
			return nil
		})
	}
}
