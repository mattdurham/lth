// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package prwatcher

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// initTestRepo creates a git repo at dir with one commit per (path, dateRFC3339)
// pair, in order, and returns the commit SHAs in the same order.
func initTestRepo(t *testing.T, dir string, commits []struct{ path, date string }) []string {
	t.Helper()
	run := func(env []string, args ...string) string {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), env...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run(nil, "init", "--quiet", "--initial-branch=main")
	run(nil, "config", "user.email", "test@example.com")
	run(nil, "config", "user.name", "Test")

	shas := make([]string, len(commits))
	for i, c := range commits {
		full := filepath.Join(dir, c.path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(full, []byte(c.path+" v"+string(rune('0'+i))), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
		run(nil, "add", c.path)
		env := []string{"GIT_AUTHOR_DATE=" + c.date, "GIT_COMMITTER_DATE=" + c.date}
		run(env, "commit", "--quiet", "-m", "commit "+c.path)
		shas[i] = run(nil, "rev-parse", "HEAD")
	}
	return shas
}

func TestCommitsSinceFiltersByTimeAndPath(t *testing.T) {
	dir := t.TempDir()
	shas := initTestRepo(t, dir, []struct{ path, date string }{
		{"a/main.jsonnet", "2024-01-01T00:00:00Z"},
		{"b/other.jsonnet", "2024-06-01T00:00:00Z"},
		{"a/main.jsonnet", "2025-01-01T00:00:00Z"},
	})

	// Since well before all commits, scoped to dir "a": only the two commits
	// touching a/, not the one touching b/.
	got, err := commitsSince(dir, "a", time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("commitsSince: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("commitsSince(dir=a) = %v, want 2 commits", got)
	}
	gotSet := map[string]bool{got[0]: true, got[1]: true}
	if !gotSet[shas[0]] || !gotSet[shas[2]] {
		t.Errorf("commitsSince(dir=a) = %v, want %v and %v", got, shas[0], shas[2])
	}

	// Since between commit 1 and commit 2, unscoped: only the most recent commit.
	got, err = commitsSince(dir, "", time.Date(2024, 12, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("commitsSince: %v", err)
	}
	if len(got) != 1 || got[0] != shas[2] {
		t.Errorf("commitsSince(since=2024-12-01) = %v, want [%v]", got, shas[2])
	}
}

func TestCommitsSinceOrderedOldestFirst(t *testing.T) {
	dir := t.TempDir()
	shas := initTestRepo(t, dir, []struct{ path, date string }{
		{"a/main.jsonnet", "2024-01-01T00:00:00Z"},
		{"a/main.jsonnet", "2024-06-01T00:00:00Z"},
		{"a/main.jsonnet", "2025-01-01T00:00:00Z"},
	})

	got, err := commitsSince(dir, "", time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("commitsSince: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("commitsSince = %v, want 3 commits", got)
	}
	for i, sha := range shas {
		if got[i] != sha {
			t.Errorf("commitsSince[%d] = %v, want %v (oldest-first order)", i, got[i], sha)
		}
	}
}

func TestCommitsSinceZeroTimeIsUnbounded(t *testing.T) {
	dir := t.TempDir()
	shas := initTestRepo(t, dir, []struct{ path, date string }{
		{"a/main.jsonnet", "2010-01-01T00:00:00Z"}, // long before any --since cutoff we'd normally use
	})

	got, err := commitsSince(dir, "", time.Time{})
	if err != nil {
		t.Fatalf("commitsSince: %v", err)
	}
	if len(got) != 1 || got[0] != shas[0] {
		t.Errorf("commitsSince(zero time) = %v, want [%v] (unbounded, full history)", got, shas[0])
	}
}

func TestEnsureFullCloneDeepensExistingShallowClone(t *testing.T) {
	remoteDir := t.TempDir()
	initTestRepo(t, remoteDir, []struct{ path, date string }{
		{"README.md", "2020-01-01T00:00:00Z"},
		{"README.md", "2021-01-01T00:00:00Z"},
	})

	cacheDir := t.TempDir()
	localPath := filepath.Join(cacheDir, "acme", "widgets")
	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Simulate a pre-existing shallow clone (e.g. left by mdwatcher's
	// markdown.github feature, which shallow-clones by default).
	// --no-local forces git to honor --depth; the default "local" clone
	// optimization for filesystem-path remotes otherwise ignores it.
	cmd := exec.Command("git", "clone", "--no-local", "--depth", "1", remoteDir, localPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("shallow clone setup: %v\n%s", err, out)
	}
	if !isShallow(localPath) {
		t.Fatalf("test setup: expected localPath to be a shallow clone")
	}

	got, err := ensureFullClone(cacheDir, "acme/widgets")
	if err != nil {
		t.Fatalf("ensureFullClone: %v", err)
	}
	if got != localPath {
		t.Errorf("ensureFullClone returned %q, want %q", got, localPath)
	}
	if isShallow(localPath) {
		t.Errorf("ensureFullClone should have deepened the shallow clone to full history")
	}

	out, err := exec.Command("git", "-C", localPath, "log", "--format=%H").Output()
	if err != nil {
		t.Fatalf("git log: %v", err)
	}
	if got := len(strings.Fields(string(out))); got != 2 {
		t.Errorf("git log after ensureFullClone found %d commits, want 2 (full history)", got)
	}
}

func TestEnsureFullCloneToleratesFetchFailureOnAlreadyClonedRepo(t *testing.T) {
	remoteDir := t.TempDir()
	initTestRepo(t, remoteDir, []struct{ path, date string }{
		{"README.md", "2020-01-01T00:00:00Z"},
		{"README.md", "2021-01-01T00:00:00Z"},
	})

	cacheDir := t.TempDir()
	localPath := filepath.Join(cacheDir, "acme", "widgets")
	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if out, err := exec.Command("git", "clone", remoteDir, localPath).CombinedOutput(); err != nil {
		t.Fatalf("initial clone: %v\n%s", err, out)
	}

	// Simulate the real production failure: a concurrent watcher (mdwatcher)
	// racing a fetch against the same shared clone. We can't reliably
	// reproduce the exact ref-lock error deterministically, so instead we
	// break the remote entirely -- from ensureFullClone's perspective, any
	// fetch failure on an already-cloned, non-shallow repo must be tolerated
	// the same way, since it always proceeds with whatever local refs exist.
	if out, err := exec.Command("git", "-C", localPath, "remote", "set-url", "origin", "/nonexistent/path").CombinedOutput(); err != nil {
		t.Fatalf("break remote: %v\n%s", err, out)
	}

	got, err := ensureFullClone(cacheDir, "acme/widgets")
	if err != nil {
		t.Fatalf("ensureFullClone should tolerate a fetch failure on an already-cloned repo, got: %v", err)
	}
	if got != localPath {
		t.Errorf("ensureFullClone returned %q, want %q", got, localPath)
	}

	// Existing history must still be intact -- reset used the local ref as-is.
	out, err := exec.Command("git", "-C", localPath, "log", "--format=%H").Output()
	if err != nil {
		t.Fatalf("git log: %v", err)
	}
	if got := len(strings.Fields(string(out))); got != 2 {
		t.Errorf("git log after tolerated fetch failure found %d commits, want 2 (existing history preserved)", got)
	}
}

func TestValidRepoSpec(t *testing.T) {
	cases := map[string]bool{
		"acme/widgets":       true,
		"example-org/repo-1": true,
		"":                   false,
		"acme":               false,
		"/widgets":           false,
		"acme/":              false,
		"acme/widgets/extra": false,
		"../etc/passwd":      false,
		"acme/.git":          false,
	}
	for spec, want := range cases {
		if got := validRepoSpec(spec); got != want {
			t.Errorf("validRepoSpec(%q) = %v, want %v", spec, got, want)
		}
	}
}

func TestCommitsSinceNoMatches(t *testing.T) {
	dir := t.TempDir()
	initTestRepo(t, dir, []struct{ path, date string }{
		{"a/main.jsonnet", "2024-01-01T00:00:00Z"},
	})

	got, err := commitsSince(dir, "", time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("commitsSince: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("commitsSince future window = %v, want empty", got)
	}
}
