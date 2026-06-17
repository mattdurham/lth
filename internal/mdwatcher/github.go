// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package mdwatcher

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/mattdurham/lth/internal/config"
)

// gitOpTimeout bounds clone/fetch/reset operations so a hung remote does
// not block the daemon's scan ticker.
const gitOpTimeout = 5 * time.Minute

// ResolvedRepo is a github repo after EnsureRepo has run successfully.
type ResolvedRepo struct {
	Spec    config.MarkdownGitHubRepo
	RootDir string // local filesystem path to the working copy
}

// EnsureRepo clones spec.Repo into cacheDir if it does not exist, otherwise
// fetches origin and hard-resets to the tracked branch. It returns the
// resolved on-disk path.
//
// Auth is delegated entirely to local git (SSH keys, credential helpers). lth
// never reads or stores credentials. A bad URL, network error, or auth
// failure surfaces as an error; the caller logs and skips this repo for the
// current cycle.
func EnsureRepo(ctx context.Context, cacheDir string, depth int, spec config.MarkdownGitHubRepo) (*ResolvedRepo, error) {
	if !validRepoSpec(spec.Repo) {
		return nil, fmt.Errorf("invalid repo spec %q (want <org>/<name>)", spec.Repo)
	}
	org, name, _ := strings.Cut(spec.Repo, "/")
	localPath := filepath.Join(cacheDir, org, name)

	if _, err := os.Stat(filepath.Join(localPath, ".git")); err != nil {
		// Not yet cloned -- shallow-clone from GitHub.
		if err := gitClone(ctx, spec, localPath, depth); err != nil {
			return nil, fmt.Errorf("clone %s: %w", spec.Repo, err)
		}
	} else {
		// Already cloned -- refresh.
		if err := gitFetchReset(ctx, spec, localPath); err != nil {
			return nil, fmt.Errorf("update %s: %w", spec.Repo, err)
		}
	}
	return &ResolvedRepo{Spec: spec, RootDir: localPath}, nil
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

func gitClone(ctx context.Context, spec config.MarkdownGitHubRepo, localPath string, depth int) error {
	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		return fmt.Errorf("mkdir parent: %w", err)
	}
	url := fmt.Sprintf("https://github.com/%s.git", spec.Repo)
	args := []string{"clone"}
	if depth > 0 {
		args = append(args, "--depth", fmt.Sprintf("%d", depth))
	}
	if spec.Branch != "" {
		args = append(args, "--branch", spec.Branch, "--single-branch")
	}
	args = append(args, url, localPath)
	return runGit(ctx, "", args...)
}

func gitFetchReset(ctx context.Context, spec config.MarkdownGitHubRepo, localPath string) error {
	if err := runGit(ctx, localPath, "fetch", "origin"); err != nil {
		return fmt.Errorf("fetch: %w", err)
	}
	branch := spec.Branch
	if branch == "" {
		// Resolve the remote's default branch from origin/HEAD.
		out, err := runGitOutput(ctx, localPath, "symbolic-ref", "--short", "refs/remotes/origin/HEAD")
		if err != nil {
			// Fall back to "main" then "master" by trying reset.
			if resetErr := runGit(ctx, localPath, "reset", "--hard", "origin/main"); resetErr == nil {
				return nil
			}
			return runGit(ctx, localPath, "reset", "--hard", "origin/master")
		}
		branch = strings.TrimPrefix(strings.TrimSpace(out), "origin/")
	}
	return runGit(ctx, localPath, "reset", "--hard", "origin/"+branch)
}

func runGit(ctx context.Context, dir string, args ...string) error {
	ctx, cancel := context.WithTimeout(ctx, gitOpTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...) //nolint:gosec // G204: args constructed from validated repo spec, not user input
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %s: %w (output: %s)", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func runGitOutput(ctx context.Context, dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, gitOpTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...) //nolint:gosec // G204: args constructed from validated repo spec, not user input
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// pathAccepted returns true if relPath (relative to repo root, forward-slash
// separated) matches at least one include glob and no exclude globs.
//
// Include semantics: if non-empty, the path must match at least one include
// pattern; if empty, all paths are eligible. Exclude overrides include.
// Supported glob syntax: `*` (segment), `?` (char), `**` (zero or more
// segments). See globMatch.
func pathAccepted(relPath string, include, exclude []string) bool {
	rp := filepath.ToSlash(relPath)
	if len(include) > 0 {
		ok := false
		for _, pat := range include {
			if globMatch(pat, rp) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	for _, pat := range exclude {
		if globMatch(pat, rp) {
			return false
		}
	}
	return true
}

// globMatch reports whether path matches glob. Glob syntax:
//
//	*       any sequence of non-slash characters within one path segment
//	?       any single non-slash character
//	**      any sequence of zero or more whole path segments (slash-spanning)
//
func globMatch(glob, path string) bool {
	g := splitGlobSegments(glob)
	p := splitGlobSegments(path)
	return matchSegs(g, p)
}

func splitGlobSegments(s string) []string {
	s = strings.Trim(filepath.ToSlash(s), "/")
	if s == "" {
		return nil
	}
	return strings.Split(s, "/")
}

// matchSegs recursively matches glob segments against path segments. A `**`
// segment consumes 0+ path segments; other segments use filepath.Match.
func matchSegs(g, p []string) bool {
	switch {
	case len(g) == 0:
		return len(p) == 0
	case g[0] == "**":
		// Collapse consecutive ** into one.
		for len(g) > 1 && g[1] == "**" {
			g = g[1:]
		}
		// Try matching 0..len(p) path segments against this **.
		for i := 0; i <= len(p); i++ {
			if matchSegs(g[1:], p[i:]) {
				return true
			}
		}
		return false
	case len(p) == 0:
		return false
	default:
		ok, err := filepath.Match(g[0], p[0])
		if err != nil || !ok {
			return false
		}
		return matchSegs(g[1:], p[1:])
	}
}

// extensionMatches reports whether path's extension (case-insensitive) is in
// allowedExts. Empty allowedExts defaults to [".md"] for back-compat.
func extensionMatches(path string, allowedExts []string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	if len(allowedExts) == 0 {
		return ext == ".md"
	}
	for _, e := range allowedExts {
		if strings.ToLower(e) == ext {
			return true
		}
	}
	return false
}

// EnsureAll resolves every configured repo, returning the successfully
// resolved set. Failures are logged but do not abort the whole batch.
func EnsureAll(ctx context.Context, cfg config.MarkdownGitHub, logger *slog.Logger) []ResolvedRepo {
	out := make([]ResolvedRepo, 0, len(cfg.Repos))
	for _, spec := range cfg.Repos {
		r, err := EnsureRepo(ctx, cfg.CacheDir, cfg.CloneDepth, spec)
		if err != nil {
			logger.Warn("mdwatcher: github repo sync failed", "repo", spec.Repo, "err", err)
			continue
		}
		logger.Info("mdwatcher: github repo ready", "repo", spec.Repo, "path", r.RootDir,
			"include", spec.Include, "exclude", spec.Exclude)
		out = append(out, *r)
	}
	return out
}
