// Package issueswatcher polls GitHub issues and ingests them as L5 memories.
package issueswatcher

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/mattdurham/lth/internal/config"
	"github.com/mattdurham/lth/internal/memory"
)

// issueState tracks what has been ingested for a single issue.
type issueState struct {
	MemoryID   string            `json:"memory_id"`
	UpdatedAt  string            `json:"updated_at"`
	CommentIDs map[string]string `json:"comment_ids"` // gh comment id → memory id
}

// repoState tracks sync state for a single repo.
type repoState struct {
	LastSync string                `json:"last_sync"` // RFC3339
	Issues   map[string]issueState `json:"issues"`    // issue number → state
}

// state is the full persisted watcher state.
type state struct {
	Repos map[string]repoState `json:"repos"`
}

// Watcher polls GitHub issues and stores them as L5 memories.
type Watcher struct {
	store     *memory.MemoryStore
	cfg       *config.Config
	stateFile string
	mu        sync.Mutex
	st        state
}

// New creates a new issues Watcher.
func New(store *memory.MemoryStore, cfg *config.Config) *Watcher {
	home, _ := os.UserHomeDir()
	return &Watcher{
		store:     store,
		cfg:       cfg,
		stateFile: filepath.Join(home, ".lth", "issues-state.json"),
	}
}

// Run polls on startup then on a ticker until ctx is cancelled.
func (w *Watcher) Run(ctx context.Context) {
	if len(w.cfg.Issues.Repos) == 0 {
		return
	}
	w.loadState()
	w.syncAll(ctx)
	interval := time.Duration(w.cfg.Issues.IntervalS) * time.Second
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			w.syncAll(ctx)
		}
	}
}

func (w *Watcher) syncAll(ctx context.Context) {
	for _, repo := range w.cfg.Issues.Repos {
		if err := w.syncRepo(ctx, repo); err != nil {
			slog.Warn("issueswatcher: sync failed", "repo", repo, "err", err)
		}
	}
}

func (w *Watcher) syncRepo(ctx context.Context, repo string) error {
	w.mu.Lock()
	rs := w.st.Repos[repo]
	since := rs.LastSync
	w.mu.Unlock()

	issues, err := fetchIssues(repo, since)
	if err != nil {
		return fmt.Errorf("fetch issues: %w", err)
	}
	if len(issues) == 0 {
		return nil
	}
	slog.Info("issueswatcher: syncing", "repo", repo, "issues", len(issues))

	for _, issue := range issues {
		if err := w.processIssue(ctx, repo, issue); err != nil {
			slog.Warn("issueswatcher: process issue failed", "repo", repo, "issue", issue.Number, "err", err)
		}
	}

	w.mu.Lock()
	if w.st.Repos == nil {
		w.st.Repos = map[string]repoState{}
	}
	rs = w.st.Repos[repo]
	rs.LastSync = time.Now().UTC().Format(time.RFC3339)
	w.st.Repos[repo] = rs
	w.mu.Unlock()
	w.saveState()
	return nil
}

func (w *Watcher) processIssue(ctx context.Context, repo string, issue ghIssue) error {
	w.mu.Lock()
	rs := w.st.Repos[repo]
	if rs.Issues == nil {
		rs.Issues = map[string]issueState{}
	}
	prev := rs.Issues[issue.NumberStr()]
	w.mu.Unlock()

	// Skip unchanged issues (same updated_at).
	if prev.UpdatedAt == issue.UpdatedAt && prev.MemoryID != "" {
		return nil
	}

	labels := issue.LabelNames()
	attrs := map[string]string{
		"source":       "github_issue",
		"repo":         repo,
		"issue_number": issue.NumberStr(),
		"issue_url":    issue.HTMLURL,
		"state":        issue.State,
		"project":      repo,
	}
	if labels != "" {
		attrs["labels"] = labels
	}

	content := fmt.Sprintf("GitHub Issue #%d [%s]: %s\n\nRepo: %s\nState: %s\nLabels: %s\nURL: %s\n\n%s",
		issue.Number, issue.State, issue.Title,
		repo, issue.State, labels, issue.HTMLURL,
		strings.TrimSpace(issue.Body))

	m, err := w.store.Store(ctx, 5, content, attrs)
	if err != nil {
		return fmt.Errorf("store issue: %w", err)
	}

	is := issueState{
		MemoryID:   m.ID,
		UpdatedAt:  issue.UpdatedAt,
		CommentIDs: prev.CommentIDs,
	}
	if is.CommentIDs == nil {
		is.CommentIDs = map[string]string{}
	}

	// Fetch and store comments.
	if issue.Comments > 0 {
		comments, err := fetchComments(repo, issue.Number)
		if err != nil {
			slog.Warn("issueswatcher: fetch comments failed", "repo", repo, "issue", issue.Number, "err", err)
		} else {
			for _, c := range comments {
				cid := fmt.Sprintf("%d", c.ID)
				if _, seen := is.CommentIDs[cid]; seen {
					continue // already stored
				}
				cAttrs := map[string]string{
					"source":          "github_issue_comment",
					"repo":            repo,
					"issue_number":    issue.NumberStr(),
					"issue_url":       issue.HTMLURL,
					"parent_issue_id": m.ID,
					"comment_id":      cid,
					"project":         repo,
				}
				cContent := fmt.Sprintf("Comment on %s#%d by %s:\n\n%s",
					repo, issue.Number, c.User.Login, strings.TrimSpace(c.Body))
				cm, err := w.store.Store(ctx, 5, cContent, cAttrs)
				if err != nil {
					slog.Warn("issueswatcher: store comment failed", "err", err)
					continue
				}
				is.CommentIDs[cid] = cm.ID
			}
		}
	}

	w.mu.Lock()
	rs = w.st.Repos[repo]
	if rs.Issues == nil {
		rs.Issues = map[string]issueState{}
	}
	rs.Issues[issue.NumberStr()] = is
	w.st.Repos[repo] = rs
	w.mu.Unlock()
	return nil
}

// --- GitHub API via gh CLI ---

type ghUser struct {
	Login string `json:"login"`
}

type ghLabel struct {
	Name string `json:"name"`
}

type ghIssue struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	State     string    `json:"state"`
	HTMLURL   string    `json:"html_url"`
	UpdatedAt string    `json:"updated_at"`
	Labels    []ghLabel `json:"labels"`
	Comments  int       `json:"comments"`
}

func (i ghIssue) NumberStr() string { return fmt.Sprintf("%d", i.Number) }
func (i ghIssue) LabelNames() string {
	names := make([]string, len(i.Labels))
	for j, l := range i.Labels {
		names[j] = l.Name
	}
	return strings.Join(names, ",")
}

type ghComment struct {
	ID        int    `json:"id"`
	Body      string `json:"body"`
	User      ghUser `json:"user"`
	CreatedAt string `json:"created_at"`
}

func fetchIssues(repo, since string) ([]ghIssue, error) {
	url := fmt.Sprintf("/repos/%s/issues?state=all&per_page=100", repo)
	if since != "" {
		url += "&since=" + since
	}
	out, err := exec.Command("gh", "api", "--paginate", url).Output()
	if err != nil {
		return nil, fmt.Errorf("gh api: %w", err)
	}
	// gh --paginate may return concatenated JSON arrays; wrap in outer array.
	var issues []ghIssue
	if err := json.Unmarshal(out, &issues); err != nil {
		return nil, fmt.Errorf("parse issues: %w", err)
	}
	return issues, nil
}

func fetchComments(repo string, issueNum int) ([]ghComment, error) {
	url := fmt.Sprintf("/repos/%s/issues/%d/comments?per_page=100", repo, issueNum)
	out, err := exec.Command("gh", "api", "--paginate", url).Output()
	if err != nil {
		return nil, fmt.Errorf("gh api comments: %w", err)
	}
	var comments []ghComment
	if err := json.Unmarshal(out, &comments); err != nil {
		return nil, fmt.Errorf("parse comments: %w", err)
	}
	return comments, nil
}

func (w *Watcher) loadState() {
	data, err := os.ReadFile(w.stateFile)
	if err != nil {
		w.st = state{Repos: map[string]repoState{}}
		return
	}
	if err := json.Unmarshal(data, &w.st); err != nil {
		w.st = state{Repos: map[string]repoState{}}
	}
}

func (w *Watcher) saveState() {
	w.mu.Lock()
	data, err := json.Marshal(&w.st)
	w.mu.Unlock()
	if err != nil {
		return
	}
	_ = os.WriteFile(w.stateFile, data, 0o600)
}
