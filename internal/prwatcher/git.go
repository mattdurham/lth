// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package prwatcher

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// commitsSince returns the commit SHAs in repoPath's current branch that
// touched dir (or the whole repo, if dir is empty), oldest first (so callers
// that cap how much they process per scan replay history in chronological
// order). A zero since means unbounded -- the entire history of dir is
// returned.
func commitsSince(repoPath, dir string, since time.Time) ([]string, error) {
	args := []string{"-C", repoPath, "log", "--reverse", "--format=%H"}
	if !since.IsZero() {
		args = append(args, "--since="+since.UTC().Format(time.RFC3339))
	}
	if dir != "" {
		args = append(args, "--", dir)
	}
	out, err := exec.Command("git", args...).Output() //nolint:gosec // repoPath/dir come from local config, not untrusted input
	if err != nil {
		return nil, fmt.Errorf("git log: %w", err)
	}
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" {
		return nil, nil
	}
	return strings.Split(trimmed, "\n"), nil
}

// pullFastForward runs a fast-forward-only pull so commitsSince sees the
// latest remote history. Used only for PRSource.Path (a checkout the caller
// owns) -- lth-managed clones use ensureFullClone's fetch+reset instead.
// Failures (detached HEAD, local edits, offline) are logged and non-fatal --
// the scan proceeds against whatever HEAD already is, matching mdwatcher's
// tolerance for an unpullable local checkout.
func pullFastForward(repoPath string) {
	out, err := exec.Command("git", "-C", repoPath, "pull", "--ff-only", "--quiet").CombinedOutput() //nolint:gosec // repoPath comes from local config
	if err != nil {
		slog.Warn("prwatcher: git pull failed", "path", repoPath, "err", err, "output", strings.TrimSpace(string(out)))
	}
}

// ensureFullClone clones repo into cacheDir/<org>/<name> if not already
// present, or fetches and resets it to the remote default branch if it is --
// deepening it first if it happens to be a shallow clone (e.g. left behind
// by mdwatcher's markdown.github feature, which shallow-clones by default).
// Always leaves the working copy as a full clone: PR history mining depends
// on seeing the whole repo, regardless of who cloned it first or how.
func ensureFullClone(cacheDir, repo string) (string, error) {
	if !validRepoSpec(repo) {
		return "", fmt.Errorf("invalid repo spec %q (want <org>/<name>)", repo)
	}
	org, name, _ := strings.Cut(repo, "/")
	localPath := filepath.Join(cacheDir, org, name)

	if _, err := os.Stat(filepath.Join(localPath, ".git")); err != nil {
		if mkErr := os.MkdirAll(filepath.Dir(localPath), 0o755); mkErr != nil {
			return "", fmt.Errorf("mkdir parent: %w", mkErr)
		}
		url := fmt.Sprintf("https://github.com/%s.git", repo)
		if cloneErr := runGit("", "clone", url, localPath); cloneErr != nil {
			return "", fmt.Errorf("clone %s: %w", repo, cloneErr)
		}
		return localPath, nil
	}

	// A fetch failure here is tolerated rather than fatal: Dir is shared with
	// mdwatcher's markdown.github feature (see NOTES.md decision #5), which
	// independently fetches/resets the same clone on its own schedule. A
	// concurrent fetch from that watcher can win a ref-update lock race on
	// e.g. refs/remotes/origin/master, making THIS fetch exit non-zero even
	// though the ref ends up correctly updated (by the other fetch) or the
	// object deepening already completed before the ref-update step lost the
	// race. Proceeding with whatever local refs already exist is always
	// safe -- reset below uses them as-is, and a stale ref just means this
	// scan sees slightly less new history, self-correcting next scan.
	if isShallow(localPath) {
		if err := runGit(localPath, "fetch", "--unshallow", "origin"); err != nil {
			if isShallow(localPath) {
				return "", fmt.Errorf("unshallow %s: %w", repo, err)
			}
			slog.Warn("prwatcher: unshallow fetch reported an error but the repo deepened successfully anyway; continuing", "repo", repo, "err", err)
		}
	} else if err := runGit(localPath, "fetch", "origin"); err != nil {
		slog.Warn("prwatcher: fetch failed, proceeding with existing local refs", "repo", repo, "err", err)
	}

	branch, err := defaultBranch(localPath)
	if err != nil {
		branch = "main"
	}
	if resetErr := runGit(localPath, "reset", "--hard", "origin/"+branch); resetErr != nil {
		if branch == "master" {
			return "", fmt.Errorf("reset %s: %w", repo, resetErr)
		}
		if fallbackErr := runGit(localPath, "reset", "--hard", "origin/master"); fallbackErr != nil {
			return "", fmt.Errorf("reset %s: %w", repo, resetErr)
		}
	}
	return localPath, nil
}

// isShallow reports whether path is a shallow git clone.
func isShallow(path string) bool {
	out, err := exec.Command("git", "-C", path, "rev-parse", "--is-shallow-repository").Output() //nolint:gosec // path is derived from validated config, not untrusted input
	return err == nil && strings.TrimSpace(string(out)) == "true"
}

// defaultBranch resolves the remote's default branch from origin/HEAD.
func defaultBranch(path string) (string, error) {
	out, err := exec.Command("git", "-C", path, "symbolic-ref", "--short", "refs/remotes/origin/HEAD").Output() //nolint:gosec // path is derived from validated config, not untrusted input
	if err != nil {
		return "", err
	}
	return strings.TrimPrefix(strings.TrimSpace(string(out)), "origin/"), nil
}

// validRepoSpec accepts non-empty "<org>/<name>" with no path traversal chars.
func validRepoSpec(s string) bool {
	if s == "" {
		return false
	}
	org, name, ok := strings.Cut(s, "/")
	if !ok || org == "" || name == "" {
		return false
	}
	for _, part := range []string{org, name} {
		if strings.ContainsAny(part, "/\\:.@ \t\n") || part == ".." {
			return false
		}
	}
	return true
}

func runGit(dir string, args ...string) error {
	cmd := exec.Command("git", args...) //nolint:gosec // args are built from validated repo specs and fixed subcommands, not untrusted input
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %s: %w (output: %s)", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}
