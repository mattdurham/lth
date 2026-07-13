// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package config

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

func writeTempConfig(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReloadInPlace_NoChange(t *testing.T) {
	path := writeTempConfig(t, "search:\n  default_top_k: 10\n  alpha: 0.333\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	changed, restart, err := ReloadInPlace(path, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(changed) != 0 || len(restart) != 0 {
		t.Errorf("expected no-change reload, got changed=%v restart=%v", changed, restart)
	}
}

func TestReloadInPlace_HotField(t *testing.T) {
	path := writeTempConfig(t, "search:\n  default_top_k: 10\n")
	cfg, _ := Load(path)
	if cfg.Search.DefaultTopK != 10 {
		t.Fatalf("setup: got %d, want 10", cfg.Search.DefaultTopK)
	}

	// Rewrite with a new top-k.
	if err := os.WriteFile(path, []byte("search:\n  default_top_k: 25\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	changed, restart, err := ReloadInPlace(path, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(changed, []string{"Search.DefaultTopK"}) {
		t.Errorf("changed = %v, want [Search.DefaultTopK]", changed)
	}
	if len(restart) != 0 {
		t.Errorf("Search.DefaultTopK is hot; restart should be empty, got %v", restart)
	}
	if cfg.Search.DefaultTopK != 25 {
		t.Errorf("dst not updated: got %d, want 25", cfg.Search.DefaultTopK)
	}
}

// TestReloadInPlace_NewlyHotFields regression-tests the adversarial-review
// finding that hotFields was missing several fields multiple watchers
// demonstrably read fresh every tick (GWS.*, Markdown.IntervalS,
// Markdown.GitHub.*, Markdown.GitPullIntervalS, Issues.IntervalS,
// Watcher.Paths), causing a config edit to be reported as "requires restart"
// even though the running watcher picks it up on its next tick without one.
func TestReloadInPlace_NewlyHotFields(t *testing.T) {
	path := writeTempConfig(t, "gws:\n  enabled: false\n")
	cfg, _ := Load(path)

	if err := os.WriteFile(path, []byte("gws:\n  enabled: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	changed, restart, err := ReloadInPlace(path, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(changed, []string{"GWS.Enabled"}) {
		t.Errorf("changed = %v, want [GWS.Enabled]", changed)
	}
	if len(restart) != 0 {
		t.Errorf("GWS.Enabled is read fresh every Run() iteration; restart should be empty, got %v", restart)
	}
}

// TestReloadInPlace_NewlyHotNestedField covers the trickiest part of the
// same fix: a field nested two levels deep (Markdown.GitHub.CacheDir) must
// diff and hot-reload as that exact dotted path, not just at the
// Markdown.GitHub struct level.
func TestReloadInPlace_NewlyHotNestedField(t *testing.T) {
	path := writeTempConfig(t, "markdown:\n  github:\n    cache_dir: /tmp/a\n")
	cfg, _ := Load(path)

	if err := os.WriteFile(path, []byte("markdown:\n  github:\n    cache_dir: /tmp/b\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	changed, restart, err := ReloadInPlace(path, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(changed, []string{"Markdown.GitHub.CacheDir"}) {
		t.Errorf("changed = %v, want [Markdown.GitHub.CacheDir]", changed)
	}
	if len(restart) != 0 {
		t.Errorf("Markdown.GitHub.CacheDir is read fresh every scanGitHubRepos call; restart should be empty, got %v", restart)
	}
}

func TestReloadInPlace_RequiresRestartField(t *testing.T) {
	path := writeTempConfig(t, "db:\n  path: /tmp/orig.db\n")
	cfg, _ := Load(path)
	origDB := cfg.DB.Path

	if err := os.WriteFile(path, []byte("db:\n  path: /tmp/new.db\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	changed, restart, err := ReloadInPlace(path, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(changed, []string{"DB.Path"}) {
		t.Errorf("changed = %v, want [DB.Path]", changed)
	}
	if !reflect.DeepEqual(restart, []string{"DB.Path"}) {
		t.Errorf("restart = %v, want [DB.Path]", restart)
	}
	if cfg.DB.Path == origDB {
		t.Errorf("dst.DB.Path was not updated even though field is non-hot")
	}
}

func TestReloadInPlace_BrokenYAMLLeavesDstAlone(t *testing.T) {
	path := writeTempConfig(t, "search:\n  default_top_k: 7\n")
	cfg, _ := Load(path)

	// Truncate to invalid YAML.
	if err := os.WriteFile(path, []byte("search:\n  default_top_k: [not, an, int\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	changed, restart, err := ReloadInPlace(path, cfg)
	if err == nil {
		t.Fatal("expected parse error, got nil")
	}
	if len(changed) != 0 || len(restart) != 0 {
		t.Errorf("changed/restart should be empty on parse error, got changed=%v restart=%v", changed, restart)
	}
	if cfg.Search.DefaultTopK != 7 {
		t.Errorf("dst was modified despite parse error: got %d, want 7", cfg.Search.DefaultTopK)
	}
}

func TestReloadInPlace_SliceField(t *testing.T) {
	path := writeTempConfig(t, "issues:\n  repos: [owner/a, owner/b]\n")
	cfg, _ := Load(path)
	if len(cfg.Issues.Repos) != 2 {
		t.Fatalf("setup: got %d repos, want 2", len(cfg.Issues.Repos))
	}

	if err := os.WriteFile(path, []byte("issues:\n  repos: [owner/a, owner/b, owner/c]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	changed, restart, err := ReloadInPlace(path, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(changed, []string{"Issues.Repos"}) {
		t.Errorf("changed = %v, want [Issues.Repos]", changed)
	}
	if len(restart) != 0 {
		t.Errorf("Issues.Repos is hot; got restart=%v", restart)
	}
	if len(cfg.Issues.Repos) != 3 {
		t.Errorf("repos not updated: %v", cfg.Issues.Repos)
	}
}

func TestReloadInPlace_MultipleChangedSorted(t *testing.T) {
	path := writeTempConfig(t, `
search:
  default_top_k: 10
compaction:
  l_5_threshold: 50
`)
	cfg, _ := Load(path)
	if err := os.WriteFile(path, []byte(`
search:
  default_top_k: 25
compaction:
  l_5_threshold: 100
db:
  path: /tmp/new.db
`), 0o600); err != nil {
		t.Fatal(err)
	}
	changed, restart, err := ReloadInPlace(path, cfg)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Compaction.L5Threshold", "DB.Path", "Search.DefaultTopK"}
	if !reflect.DeepEqual(changed, want) {
		t.Errorf("changed = %v, want %v (must be sorted)", changed, want)
	}
	if !sort.StringsAreSorted(changed) {
		t.Errorf("changed not sorted: %v", changed)
	}
	if !reflect.DeepEqual(restart, []string{"DB.Path"}) {
		t.Errorf("restart = %v, want [DB.Path]", restart)
	}
}
