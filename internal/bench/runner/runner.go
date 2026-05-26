// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package runner

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/mattdurham/lth/internal/bench/dataset"
	"github.com/mattdurham/lth/internal/bench/patcher"
)

// Config holds runtime settings for the Runner.

// timeout per claude invocation, default 10m
// claude model name, e.g. "claude-sonnet-4-6" (empty = claude default)
// root of repo cache, default ~/.cache/swe-bench

// Runner invokes claude for one problem × approach at a time.
type Runner struct {
	cfg Config
}

// New returns a Runner with the given config.
func New(cfg Config) *Runner {
	if cfg.ClaudeTimeout == 0 {
		cfg.ClaudeTimeout = 10 * time.Minute
	}
	if cfg.CacheDir == "" {
		home, _ := os.UserHomeDir()
		cfg.CacheDir = filepath.Join(home, ".cache", "swe-bench")
	}
	return &Runner{cfg: cfg}
}

// RunOne clones the target repo, applies the test patch, runs claude in the
// real source tree, then captures git diff as the patch. Never panics or
// returns an error — all failures are encoded in Result.Outcome.
func (r *Runner) RunOne(ctx context.Context, problem dataset.Problem, approach Approach) Result {
	start := time.Now()

	// 1. Clone repo to cache (idempotent) and create a throwaway worktree.
	cacheDir, err := r.cloneRepo(ctx, problem.Repo)
	if err != nil {
		return failResult(problem.InstanceID, string(approach), OutcomeCloneFail, err, start)
	}

	workDir, err := os.MkdirTemp("", "swe-bench-*")
	if err != nil {
		return failResult(problem.InstanceID, string(approach), OutcomeCloneFail, err, start)
	}
	defer os.RemoveAll(workDir)
	repoDir := filepath.Join(workDir, "repo")

	if err := r.addWorktree(ctx, cacheDir, problem.BaseCommit, repoDir); err != nil {
		return failResult(problem.InstanceID, string(approach), OutcomeCloneFail, err, start)
	}
	defer r.removeWorktree(cacheDir, repoDir) //nolint:errcheck

	// 2. Apply test_patch so the failing tests exist in the tree.
	if problem.TestPatch != "" {
		if err := patcher.ApplyPatch(ctx, repoDir, problem.TestPatch); err != nil {
			return failResult(problem.InstanceID, string(approach), OutcomeTestPatchFail, err, start)
		}
	}

	// 3. Run claude inside the real repo — agents can read/write actual files.
	claudeCtx, cancel := context.WithTimeout(ctx, r.cfg.ClaudeTimeout)
	defer cancel()

	prompt := approach.BuildPrompt(problem)
	if _, err := r.runClaude(claudeCtx, repoDir, prompt); err != nil {
		return failResult(problem.InstanceID, string(approach), OutcomeClaudeFail, err, start)
	}

	// 4. Capture what changed via git diff — real hashes, correct line numbers.
	patch, err := r.gitDiff(ctx, repoDir)
	if err != nil || strings.TrimSpace(patch) == "" {
		return Result{
			InstanceID:  problem.InstanceID,
			Approach:    string(approach),
			Outcome:     OutcomeNoPatch,
			DurationSec: time.Since(start).Seconds(),
			StartedAt:   start,
		}
	}

	return Result{
		InstanceID:  problem.InstanceID,
		Approach:    string(approach),
		Outcome:     OutcomePatchGenerated,
		ModelPatch:  patch,
		DurationSec: time.Since(start).Seconds(),
		StartedAt:   start,
	}
}

// cloneRepo clones the GitHub repo to cfg.CacheDir using blobless clone.
// Idempotent — skips clone if the directory already exists.
func (r *Runner) cloneRepo(ctx context.Context, repo string) (string, error) {
	cacheDir := filepath.Join(r.cfg.CacheDir, strings.ReplaceAll(repo, "/", "_"))
	if _, err := os.Stat(cacheDir); err == nil {
		return cacheDir, nil
	}
	if err := os.MkdirAll(r.cfg.CacheDir, 0o755); err != nil {
		return "", fmt.Errorf("create cache dir: %w", err)
	}
	cmd := exec.CommandContext(ctx, "git", "clone", "--filter=blob:none",
		"https://github.com/"+repo, cacheDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("git clone %s: %w\n%s", repo, err, out)
	}
	return cacheDir, nil
}

// addWorktree creates a detached git worktree at workDir from cacheDir at commit.
func (r *Runner) addWorktree(ctx context.Context, cacheDir, commit, workDir string) error {
	cmd := exec.CommandContext(ctx, "git", "worktree", "add", "--detach", workDir, commit)
	cmd.Dir = cacheDir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git worktree add: %w\n%s", err, out)
	}
	return nil
}

// removeWorktree cleans up the worktree after use.
func (r *Runner) removeWorktree(cacheDir, workDir string) error {
	cmd := exec.Command("git", "worktree", "remove", "--force", workDir)
	cmd.Dir = cacheDir
	return cmd.Run()
}

// gitDiff returns the unified diff of changes made in repoDir vs HEAD.
func (r *Runner) gitDiff(ctx context.Context, repoDir string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "diff", "HEAD")
	cmd.Dir = repoDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git diff: %w\n%s", err, out)
	}
	return string(out), nil
}

// runClaude invokes claude in repoDir so agents work on the real source tree.
func (r *Runner) runClaude(ctx context.Context, repoDir, prompt string) (string, error) {
	args := []string{"-p", "--dangerously-skip-permissions"}
	if r.cfg.Model != "" {
		args = append(args, "--model", r.cfg.Model)
	}

	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Dir = repoDir
	cmd.Stdin = strings.NewReader(prompt)
	cmd.Env = append(os.Environ(), "CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS=1")
	cmd.WaitDelay = 5 * time.Second

	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("claude: %w", err)
	}
	return string(out), nil
}

func failResult(instanceID, approach string, outcome Outcome, err error, start time.Time) Result {
	return Result{
		InstanceID:  instanceID,
		Approach:    approach,
		Outcome:     outcome,
		DurationSec: time.Since(start).Seconds(),
		Error:       err.Error(),
		StartedAt:   start,
	}
}
