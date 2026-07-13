// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(""), 0o600); err != nil {
		t.Fatalf("write empty config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	def := Default()

	if cfg.DB.Path != def.DB.Path {
		t.Errorf("DB.Path = %q, want %q", cfg.DB.Path, def.DB.Path)
	}
	if cfg.Embedding.BaseURL != def.Embedding.BaseURL {
		t.Errorf("Embedding.BaseURL = %q, want %q", cfg.Embedding.BaseURL, def.Embedding.BaseURL)
	}
	if EmbeddingModel != EmbeddingModel {
		t.Errorf("Embedding.Model = %q, want %q", EmbeddingModel, EmbeddingModel)
	}
	if cfg.Embedding.TimeoutS != def.Embedding.TimeoutS {
		t.Errorf("Embedding.TimeoutS = %d, want %d", cfg.Embedding.TimeoutS, def.Embedding.TimeoutS)
	}
	if cfg.LLM.BaseURL != def.LLM.BaseURL {
		t.Errorf("LLM.BaseURL = %q, want %q", cfg.LLM.BaseURL, def.LLM.BaseURL)
	}
	if cfg.LLM.Model != def.LLM.Model {
		t.Errorf("LLM.Model = %q, want %q", cfg.LLM.Model, def.LLM.Model)
	}
	if cfg.Compaction.L5Threshold != def.Compaction.L5Threshold {
		t.Errorf("Compaction.L5Threshold = %d, want %d", cfg.Compaction.L5Threshold, def.Compaction.L5Threshold)
	}
	if cfg.Search.DefaultTopK != def.Search.DefaultTopK {
		t.Errorf("Search.DefaultTopK = %d, want %d", cfg.Search.DefaultTopK, def.Search.DefaultTopK)
	}
}

func TestLoadOverrides(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `db:
  path: "/custom/memory.db"
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.DB.Path != "/custom/memory.db" {
		t.Errorf("DB.Path = %q, want /custom/memory.db", cfg.DB.Path)
	}

	def := Default()
	if cfg.Embedding.BaseURL != def.Embedding.BaseURL {
		t.Errorf("Embedding.BaseURL = %q, want %q (default should apply)", cfg.Embedding.BaseURL, def.Embedding.BaseURL)
	}
}

func TestLoadInvalid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("key: [invalid"), 0o600); err != nil {
		t.Fatalf("write invalid config: %v", err)
	}

	cfg, err := Load(path)
	if err == nil {
		t.Error("Load(invalid YAML): expected error, got nil")
	}
	if cfg != nil {
		t.Errorf("Load(invalid YAML): expected nil cfg, got %+v", cfg)
	}
}

func TestLoadMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nonexistent.yaml")

	cfg, err := Load(path)
	if err == nil {
		t.Error("Load(missing): expected error, got nil")
	}
	if cfg != nil {
		t.Errorf("Load(missing): expected nil cfg, got %+v", cfg)
	}
}

func TestConfigPath(t *testing.T) {
	path, err := ConfigPath()
	if err != nil {
		t.Fatalf("ConfigPath: %v", err)
	}
	if !strings.Contains(path, ".lth") {
		t.Errorf("ConfigPath = %q, want path containing .lth", path)
	}
	if !strings.Contains(path, "config.yaml") {
		t.Errorf("ConfigPath = %q, want path containing config.yaml", path)
	}
}

func TestDefault(t *testing.T) {
	cfg := Default()

	if cfg == nil {
		t.Fatal("Default() returned nil")
	}
	if !strings.Contains(cfg.DB.Path, ".lth") {
		t.Errorf("DB.Path = %q, want path containing .lth", cfg.DB.Path)
	}
	if cfg.Embedding.BaseURL != "http://localhost:8080" {
		t.Errorf("Embedding.BaseURL = %q, want http://localhost:8080", cfg.Embedding.BaseURL)
	}
	if EmbeddingModel != "nomic-ai/nomic-embed-text-v1.5" {
		t.Errorf("Embedding.Model = %q, want nomic-ai/nomic-embed-text-v1.5", EmbeddingModel)
	}
	if cfg.Embedding.TimeoutS != 30 {
		t.Errorf("Embedding.TimeoutS = %d, want 30", cfg.Embedding.TimeoutS)
	}
	if cfg.LLM.Model == "" {
		t.Error("LLM.Model is empty")
	}
	if cfg.Compaction.L5Threshold != 50 {
		t.Errorf("Compaction.L5Threshold = %d, want 50", cfg.Compaction.L5Threshold)
	}
	if cfg.Compaction.L5ClusterThreshold != 0.75 {
		t.Errorf("Compaction.L5ClusterThreshold = %f, want 0.75", cfg.Compaction.L5ClusterThreshold)
	}
	if cfg.Compaction.L5MinClusterSize != 2 {
		t.Errorf("Compaction.L5MinClusterSize = %d, want 2", cfg.Compaction.L5MinClusterSize)
	}
	if cfg.Search.DefaultTopK != 10 {
		t.Errorf("Search.DefaultTopK = %d, want 10", cfg.Search.DefaultTopK)
	}
	if cfg.Search.Alpha <= 0 {
		t.Errorf("Search.Alpha = %f, want > 0", cfg.Search.Alpha)
	}
}

func TestInitDefault(t *testing.T) {
	t.Run("creates new file", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.yaml")
		if err := InitDefault(path, false); err != nil {
			t.Fatalf("InitDefault: %v", err)
		}
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("file should exist: %v", err)
		}
		// File should be loadable as valid TOML.
		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load after InitDefault: %v", err)
		}
		if cfg == nil {
			t.Fatal("Load returned nil after InitDefault")
		}
	})

	t.Run("returns error if file exists without force", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.yaml")
		if err := InitDefault(path, false); err != nil {
			t.Fatalf("first InitDefault: %v", err)
		}
		err := InitDefault(path, false)
		if err == nil {
			t.Error("second InitDefault without force: expected error, got nil")
		}
	})

	t.Run("force overwrites existing file", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.yaml")
		if err := InitDefault(path, false); err != nil {
			t.Fatalf("first InitDefault: %v", err)
		}
		if err := InitDefault(path, true); err != nil {
			t.Fatalf("InitDefault with force: %v", err)
		}
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("file should still exist after force: %v", err)
		}
	})

	t.Run("creates intermediate directories", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "nested", "subdir", "config.yaml")
		if err := InitDefault(path, false); err != nil {
			t.Fatalf("InitDefault with nested path: %v", err)
		}
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("file should exist in nested dir: %v", err)
		}
	})
}

// TestDefaultPRCacheDirNotSharedWithMarkdownGitHub regression-tests the fix
// for a real production incident: prwatcher and mdwatcher both auto-cloning
// into the same cache directory caused git-reset races that made mdwatcher
// misread transient file absence as permanent deletion, soft-deleting
// hundreds of memories. PR.CacheDir must always default to its own
// dedicated directory, never the one mdwatcher's GitHub-repos feature uses.
func TestDefaultPRCacheDirNotSharedWithMarkdownGitHub(t *testing.T) {
	cfg := Default()
	if cfg.PR.CacheDir == "" {
		t.Fatal("PR.CacheDir should have a non-empty default")
	}
	if cfg.PR.CacheDir == cfg.Markdown.GitHub.CacheDir {
		t.Errorf("PR.CacheDir (%q) must not default to the same directory as Markdown.GitHub.CacheDir (%q)",
			cfg.PR.CacheDir, cfg.Markdown.GitHub.CacheDir)
	}
}

// TestInitDefaultTemplateMatchesDefaults guards against drift between
// Default()'s Go values (both the DefaultPR*/DefaultBackup* constants and
// the plain literals set directly in Default()) and InitDefault's template
// string, which hand-retypes every one of them as YAML -- active for
// embedding/llm/compaction/search, commented-out example text for the rest.
// A generated config template can't reference the Go values directly, so
// this is the only thing catching a future default change that forgets to
// update the template text to match.
func TestInitDefaultTemplateMatchesDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := InitDefault(path, false); err != nil {
		t.Fatalf("InitDefault: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read generated config: %v", err)
	}
	content := string(data)
	def := Default()

	for _, want := range []string{
		fmt.Sprintf("timeout_s: %d", def.Embedding.TimeoutS),
		fmt.Sprintf("docker_port: %d", def.Embedding.DockerPort),
		fmt.Sprintf("timeout_s: %d", def.LLM.TimeoutS),
		fmt.Sprintf("interval_s: %d", def.Compaction.IntervalS),
		fmt.Sprintf("l5_threshold: %d", def.Compaction.L5Threshold),
		fmt.Sprintf("l5_max_age_h: %d", def.Compaction.L5MaxAgeH),
		fmt.Sprintf("l5_cluster_threshold: %g", def.Compaction.L5ClusterThreshold),
		fmt.Sprintf("l5_min_cluster_size: %d", def.Compaction.L5MinClusterSize),
		fmt.Sprintf("l5_max_cluster_chars: %d", def.Compaction.L5MaxClusterChars),
		fmt.Sprintf("l4_cluster_size: %d", def.Compaction.L4ClusterSize),
		fmt.Sprintf("l3_episodes_min: %d", def.Compaction.L3EpisodesMin),
		fmt.Sprintf("l3_importance_min: %g", def.Compaction.L3ImportanceMin),
		fmt.Sprintf("seed_min_l2: %d", def.Compaction.SeedMinL2),
		fmt.Sprintf("seed_min_l3: %d", def.Compaction.SeedMinL3),
		fmt.Sprintf("seed_sample: %d", def.Compaction.SeedSample),
		fmt.Sprintf("valence_compaction_min: %g", def.Compaction.ValenceCompactionMin),
		fmt.Sprintf("default_top_k: %d", def.Search.DefaultTopK),
		fmt.Sprintf("alpha: %g", def.Search.Alpha),
		fmt.Sprintf("beta: %g", def.Search.Beta),
		fmt.Sprintf("gamma: %g", def.Search.Gamma),
		fmt.Sprintf("log_retain_days: %d", def.Watcher.LogRetainDays),
		fmt.Sprintf("interval_h: %d", def.GWS.IntervalH),
		fmt.Sprintf("lookback_days: %d", def.GWS.LookbackDays),
		fmt.Sprintf("interval_s: %d", DefaultPRIntervalS),
		fmt.Sprintf("layer: %d", DefaultPRLayer),
		fmt.Sprintf("max_per_scan: %d", DefaultPRMaxPerScan),
		fmt.Sprintf("interval_h: %d", DefaultBackupIntervalH),
		fmt.Sprintf("keep: %d", DefaultBackupKeep),
	} {
		if !strings.Contains(content, want) {
			t.Errorf("generated config template missing/drifted from %q -- update InitDefault's template string in load.go to match", want)
		}
	}
}

func TestApplyDefaultsComplete(t *testing.T) {
	// Empty config: all fields should receive defaults after applyDefaults.
	cfg := &Config{}
	applyDefaults(cfg)

	def := Default()

	if cfg.DB.Path != def.DB.Path {
		t.Errorf("DB.Path = %q, want %q", cfg.DB.Path, def.DB.Path)
	}
	if cfg.Embedding.BaseURL != def.Embedding.BaseURL {
		t.Errorf("Embedding.BaseURL = %q, want %q", cfg.Embedding.BaseURL, def.Embedding.BaseURL)
	}
	if EmbeddingModel != EmbeddingModel {
		t.Errorf("Embedding.Model = %q, want %q", EmbeddingModel, EmbeddingModel)
	}
	if cfg.Embedding.TimeoutS != def.Embedding.TimeoutS {
		t.Errorf("Embedding.TimeoutS = %d, want %d", cfg.Embedding.TimeoutS, def.Embedding.TimeoutS)
	}
	if cfg.LLM.BaseURL != def.LLM.BaseURL {
		t.Errorf("LLM.BaseURL = %q, want %q", cfg.LLM.BaseURL, def.LLM.BaseURL)
	}
	if cfg.LLM.Model != def.LLM.Model {
		t.Errorf("LLM.Model = %q, want %q", cfg.LLM.Model, def.LLM.Model)
	}
	if cfg.LLM.TimeoutS != def.LLM.TimeoutS {
		t.Errorf("LLM.TimeoutS = %d, want %d", cfg.LLM.TimeoutS, def.LLM.TimeoutS)
	}
	if cfg.Compaction.IntervalS != def.Compaction.IntervalS {
		t.Errorf("Compaction.IntervalS = %d, want %d", cfg.Compaction.IntervalS, def.Compaction.IntervalS)
	}
	if cfg.Compaction.L5Threshold != 50 {
		t.Errorf("Compaction.L5Threshold = %d, want 50", cfg.Compaction.L5Threshold)
	}
	if cfg.Compaction.L5ClusterThreshold != 0.75 {
		t.Errorf("Compaction.L5ClusterThreshold = %f, want 0.75", cfg.Compaction.L5ClusterThreshold)
	}
	if cfg.Compaction.L5MinClusterSize != 2 {
		t.Errorf("Compaction.L5MinClusterSize = %d, want 2", cfg.Compaction.L5MinClusterSize)
	}
	if cfg.Compaction.L5MaxAgeH != def.Compaction.L5MaxAgeH {
		t.Errorf("Compaction.L5MaxAgeH = %d, want %d", cfg.Compaction.L5MaxAgeH, def.Compaction.L5MaxAgeH)
	}
	if cfg.Compaction.L4ClusterSize != 5 {
		t.Errorf("Compaction.L4ClusterSize = %d, want 5", cfg.Compaction.L4ClusterSize)
	}
	if cfg.Compaction.L3EpisodesMin != def.Compaction.L3EpisodesMin {
		t.Errorf("Compaction.L3EpisodesMin = %d, want %d", cfg.Compaction.L3EpisodesMin, def.Compaction.L3EpisodesMin)
	}
	if cfg.Compaction.L3ImportanceMin != def.Compaction.L3ImportanceMin {
		t.Errorf("Compaction.L3ImportanceMin = %f, want %f", cfg.Compaction.L3ImportanceMin, def.Compaction.L3ImportanceMin)
	}
	if cfg.Search.Alpha == 0 {
		t.Error("Search.Alpha should be non-zero after applyDefaults")
	}
	if cfg.Search.Beta == 0 {
		t.Error("Search.Beta should be non-zero after applyDefaults")
	}
	if cfg.Search.Gamma == 0 {
		t.Error("Search.Gamma should be non-zero after applyDefaults")
	}
	if len(cfg.Watcher.Paths) == 0 {
		t.Error("Watcher.Paths should be non-empty after applyDefaults")
	}
	if cfg.Watcher.StateFile == "" {
		t.Error("Watcher.StateFile should be non-empty after applyDefaults")
	}
}

func TestApplyDefaultsPreservesExisting(t *testing.T) {
	// Non-zero fields must NOT be overwritten by applyDefaults.
	cfg := &Config{}
	cfg.DB.Path = "/custom/path.db"
	cfg.Embedding.BaseURL = "http://custom:8080"
	cfg.Embedding.TimeoutS = 99
	cfg.LLM.BaseURL = "http://custom-llm:8080"
	cfg.LLM.Model = "custom-llm-model"
	cfg.LLM.TimeoutS = 42
	cfg.Compaction.IntervalS = 7200
	cfg.Compaction.L5Threshold = 100
	cfg.Compaction.L5MaxAgeH = 48
	cfg.Compaction.L4ClusterSize = 10
	cfg.Compaction.L3EpisodesMin = 20
	cfg.Compaction.L3ImportanceMin = 8.0
	cfg.Search.DefaultTopK = 20
	cfg.Search.Alpha = 0.5
	cfg.Search.Beta = 0.3
	cfg.Search.Gamma = 0.2
	cfg.Watcher.Paths = []string{"/custom/path"}
	cfg.Watcher.StateFile = "/custom/state.json"

	applyDefaults(cfg)

	if cfg.DB.Path != "/custom/path.db" {
		t.Errorf("DB.Path overwritten: got %q", cfg.DB.Path)
	}
	if cfg.Embedding.BaseURL != "http://custom:8080" {
		t.Errorf("Embedding.BaseURL overwritten: got %q", cfg.Embedding.BaseURL)
	}
	if cfg.Compaction.L5Threshold != 100 {
		t.Errorf("Compaction.L5Threshold overwritten: got %d", cfg.Compaction.L5Threshold)
	}
	if cfg.Search.Alpha != 0.5 {
		t.Errorf("Search.Alpha overwritten: got %f", cfg.Search.Alpha)
	}
	if cfg.Watcher.StateFile != "/custom/state.json" {
		t.Errorf("Watcher.StateFile overwritten: got %q", cfg.Watcher.StateFile)
	}
}
