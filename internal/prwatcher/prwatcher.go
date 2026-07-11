// Package prwatcher mines merged PR history for configured repos -- cloning
// and updating them itself unless pointed at an existing local checkout --
// and stores an LLM-written summary of each new PR as a memory, backdated to
// the PR's merge time so old PRs decay in search like old memories instead
// of scoring as freshly created.
//
// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.
package prwatcher

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mattdurham/lth/internal/config"
	"github.com/mattdurham/lth/internal/llm"
	"github.com/mattdurham/lth/internal/memory"
	"github.com/mattdurham/lth/internal/metrics"
)

// prRecord tracks a PR whose fate (summarized or deliberately skipped) has
// been decided for good.
type prRecord struct {
	MemoryID string `json:"memory_id"` // empty when Skipped
	MergedAt string `json:"merged_at"`
	Skipped  bool   `json:"skipped,omitempty"`
}

// sourceState tracks ingestion progress for one configured PRSource.
type sourceState struct {
	// SummarizedPRs maps PR number -> outcome, for PRs whose fate is decided.
	SummarizedPRs map[string]prRecord `json:"summarized_prs"`
	// SeenCommits marks commit SHAs whose PR resolution is terminal, so they
	// are not re-resolved via `gh api` on every scan. A commit belonging to a
	// still-open PR is deliberately left unmarked so it gets rechecked once
	// merged.
	SeenCommits map[string]bool `json:"seen_commits,omitempty"`
}

// state is the full persisted watcher state, keyed by "repo|dir".
type state struct {
	Sources map[string]sourceState `json:"sources"`
}

// prOutcome is the result of attempting to summarize one PR.
type prOutcome struct {
	Stored   bool // a new memory was written
	Terminal bool // fate decided for good; caller should mark commits seen and record the outcome
	MemoryID string
	MergedAt string
}

// Watcher mines merged PR history and stores LLM-written summaries as memories.
type Watcher struct {
	store     *memory.MemoryStore
	llm       llm.LLM
	cfg       *config.Config
	stateFile string
	metrics   *metrics.Metrics
	mu        sync.Mutex
	st        state
}

// New creates a PR Watcher.
func New(store *memory.MemoryStore, l llm.LLM, cfg *config.Config, m *metrics.Metrics) *Watcher {
	home, _ := os.UserHomeDir()
	return &Watcher{
		store:     store,
		llm:       l,
		cfg:       cfg,
		metrics:   m,
		stateFile: filepath.Join(home, ".lth", "pr-state.json"),
	}
}

// Run is hot-reload friendly: it loops forever, checking cfg.PR.Sources on
// each iteration. When empty, it sleeps for 60s and re-checks; new sources
// added via config hot-reload are picked up on the next tick without
// requiring a daemon restart. Returns only on ctx cancellation.
func (w *Watcher) Run(ctx context.Context) {
	const disabledPoll = 60 * time.Second
	stateLoaded := false
	for {
		if len(w.cfg.PR.Sources) == 0 {
			if !sleepCtx(ctx, disabledPoll) {
				return
			}
			continue
		}
		if !stateLoaded {
			w.loadState()
			stateLoaded = true
		}
		w.scanAll(ctx)
		interval := time.Duration(w.cfg.PR.IntervalS) * time.Second
		if interval <= 0 {
			interval = 6 * time.Hour
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

// scanAll scans every configured source, sharing one per-scan budget across
// all of them so a single source with a large (or, with LookbackDays=0,
// unbounded) backlog cannot starve the others or burst a large batch of
// gh/LLM calls in one tick. A source too big to finish in one scan simply
// continues on the next -- see internal/prwatcher/NOTES.md decision #1.
func (w *Watcher) scanAll(ctx context.Context) {
	budget := w.cfg.PR.MaxPerScan
	if budget <= 0 {
		budget = 10
	}
	for _, src := range w.cfg.PR.Sources {
		if budget <= 0 {
			break
		}
		attempted, err := w.scanSource(ctx, src, budget)
		if err != nil {
			slog.Warn("prwatcher: scan source failed", "repo", src.Repo, "err", err)
			continue
		}
		budget -= attempted
	}
}

// resolveSourcePath returns the local git checkout to mine for src. If
// src.Path is set, it is used as-is (fast-forward-pulled, never cloned or
// reset -- the caller owns it). Otherwise lth clones/updates the repo itself
// into Markdown.GitHub.CacheDir as a full clone.
func (w *Watcher) resolveSourcePath(src config.PRSource) (string, error) {
	if src.Path != "" {
		path := expandHome(src.Path)
		pullFastForward(path)
		return path, nil
	}
	cacheDir := expandHome(w.cfg.Markdown.GitHub.CacheDir)
	return ensureFullClone(cacheDir, src.Repo)
}

// scanSource mines up to budget new PRs from src and returns how many it
// attempted (resolved and either stored, skipped, or found still-open) --
// this is the figure scanAll subtracts from the shared cross-source budget,
// since resolving a commit costs a `gh api` call regardless of whether the
// PR it belongs to ends up stored.
func (w *Watcher) scanSource(ctx context.Context, src config.PRSource, budget int) (int, error) {
	path, err := w.resolveSourcePath(src)
	if err != nil {
		return 0, fmt.Errorf("resolve path: %w", err)
	}

	key := src.Repo + "|" + src.Dir
	w.mu.Lock()
	rs := w.st.Sources[key]
	w.mu.Unlock()

	var since time.Time // zero value = unbounded (LookbackDays <= 0)
	if w.cfg.PR.LookbackDays > 0 {
		since = time.Now().UTC().Add(-time.Duration(w.cfg.PR.LookbackDays) * 24 * time.Hour)
	}

	shas, err := commitsSince(path, src.Dir, since)
	if err != nil {
		return 0, fmt.Errorf("git log: %w", err)
	}

	newPRs, prCommits := discoverNewPRs(&rs, shas, budget, func(sha string) (int, bool, error) {
		return resolvePRForCommit(src.Repo, sha)
	})

	for _, num := range newPRs {
		outcome, sumErr := w.summarizePR(ctx, src, num)
		if sumErr != nil {
			slog.Warn("prwatcher: summarize PR failed", "repo", src.Repo, "pr", num, "err", sumErr)
			continue
		}
		if outcome.Terminal {
			recordOutcome(&rs, num, outcome)
			for _, sha := range prCommits[num] {
				markSeen(&rs, sha)
			}
		}
	}

	w.mu.Lock()
	if w.st.Sources == nil {
		w.st.Sources = map[string]sourceState{}
	}
	w.st.Sources[key] = rs
	w.mu.Unlock()
	w.saveState()
	if w.metrics != nil {
		w.metrics.PRLastSync.WithLabelValues(src.Repo).SetToCurrentTime()
	}
	return len(newPRs), nil
}

// discoverNewPRs walks shas (oldest first) and resolves each not-yet-seen
// commit to a PR number via resolve, stopping once budget distinct
// not-yet-decided PRs have been found -- bounding gh-api resolve calls to
// roughly budget per scan regardless of how large shas is (e.g. with an
// unbounded LookbackDays). A commit with no associated PR, or belonging to
// an already-decided PR, is marked seen immediately via rs and excluded from
// the result. Returns the newly discovered PR numbers in discovery
// (chronological) order, plus the commits found for each.
func discoverNewPRs(rs *sourceState, shas []string, budget int, resolve func(sha string) (num int, ok bool, err error)) ([]int, map[int][]string) {
	prCommits := map[int][]string{}
	var newPRs []int
	for _, sha := range shas {
		if len(newPRs) >= budget {
			break
		}
		if rs.SeenCommits[sha] {
			continue
		}
		num, ok, err := resolve(sha)
		if err != nil {
			slog.Warn("prwatcher: resolve PR for commit failed", "sha", sha, "err", err)
			continue // transient -- retry next scan
		}
		if !ok {
			// Direct push, never part of any PR -- terminal, stop checking it.
			markSeen(rs, sha)
			continue
		}
		if _, done := rs.SummarizedPRs[strconv.Itoa(num)]; done {
			markSeen(rs, sha)
			continue
		}
		if _, known := prCommits[num]; !known {
			newPRs = append(newPRs, num)
		}
		prCommits[num] = append(prCommits[num], sha)
	}
	return newPRs, prCommits
}

// markSeen records sha as terminally resolved, initializing the map if needed.
func markSeen(rs *sourceState, sha string) {
	if rs.SeenCommits == nil {
		rs.SeenCommits = map[string]bool{}
	}
	rs.SeenCommits[sha] = true
}

// recordOutcome records a decided PR fate, initializing the map if needed.
func recordOutcome(rs *sourceState, number int, outcome prOutcome) {
	if rs.SummarizedPRs == nil {
		rs.SummarizedPRs = map[string]prRecord{}
	}
	rs.SummarizedPRs[strconv.Itoa(number)] = prRecord{
		MemoryID: outcome.MemoryID,
		MergedAt: outcome.MergedAt,
		Skipped:  !outcome.Stored,
	}
}

// summarizePR fetches PR metadata and, if merged and not bot-authored,
// summarizes its diff via the LLM and stores the result. A still-open PR
// returns a non-terminal outcome so it is rechecked on the next scan.
func (w *Watcher) summarizePR(ctx context.Context, src config.PRSource, number int) (prOutcome, error) {
	pr, err := fetchPR(src.Repo, number)
	if err != nil {
		return prOutcome{}, fmt.Errorf("fetch pr: %w", err)
	}
	if pr.State != "MERGED" || pr.MergedAt == "" {
		return prOutcome{}, nil // still open -- recheck next scan
	}
	if isSkippedAuthor(pr.Author.Login, w.cfg.PR.SkipAuthors) {
		return prOutcome{Terminal: true, MergedAt: pr.MergedAt}, nil
	}

	diff, diffErr := fetchDiff(src.Repo, number)
	if diffErr != nil {
		slog.Warn("prwatcher: fetch diff failed, summarizing without it", "repo", src.Repo, "pr", number, "err", diffErr)
		diff = ""
	}
	diff = truncateDiff(diff, maxDiffChars)

	prompt := buildPrompt(src.Repo, src.Dir, pr.Title, pr.Body, pr.Author.Login, diff)
	summary, err := w.llm.Complete(ctx, prompt)
	if err != nil {
		return prOutcome{}, fmt.Errorf("llm: %w", err) // transient -- retry next scan
	}
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return prOutcome{}, errors.New("llm returned empty summary")
	}

	attrs := map[string]string{
		"source":     "github_pr",
		"project":    src.Repo,
		"repo":       src.Repo,
		"pr_number":  strconv.Itoa(number),
		"pr_url":     pr.URL,
		"pr_author":  pr.Author.Login,
		"created_at": pr.MergedAt,
	}
	if src.Dir != "" {
		attrs["dir"] = src.Dir
	}

	layer := w.cfg.PR.Layer
	if layer < 1 || layer > 5 {
		layer = 5
	}

	mem, err := w.store.Store(ctx, layer, summary, attrs)
	if err != nil {
		return prOutcome{}, fmt.Errorf("store: %w", err)
	}
	if w.metrics != nil {
		w.metrics.PRIngestedTotal.WithLabelValues(src.Repo).Inc()
	}

	return prOutcome{Stored: true, Terminal: true, MemoryID: mem.ID, MergedAt: pr.MergedAt}, nil
}

// isSkippedAuthor reports whether login matches one of the configured bot logins.
func isSkippedAuthor(login string, skipAuthors []string) bool {
	for _, s := range skipAuthors {
		if strings.EqualFold(login, s) {
			return true
		}
	}
	return false
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

func (w *Watcher) loadState() {
	data, err := os.ReadFile(w.stateFile)
	if err != nil {
		w.st = state{Sources: map[string]sourceState{}}
		return
	}
	if err := json.Unmarshal(data, &w.st); err != nil {
		w.st = state{Sources: map[string]sourceState{}}
	}
}

func (w *Watcher) saveState() {
	w.mu.Lock()
	data, err := json.Marshal(&w.st)
	w.mu.Unlock()
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(w.stateFile), 0o755)
	_ = os.WriteFile(w.stateFile, data, 0o600)
}
