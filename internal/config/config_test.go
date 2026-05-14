// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
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
	if cfg.Embedding.Model != def.Embedding.Model {
		t.Errorf("Embedding.Model = %q, want %q", cfg.Embedding.Model, def.Embedding.Model)
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
	path := filepath.Join(dir, "config.toml")
	content := `[db]
path = "/custom/memory.db"
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
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("key = [invalid"), 0o600); err != nil {
		t.Fatalf("write invalid config: %v", err)
	}

	cfg, err := Load(path)
	if err == nil {
		t.Error("Load(invalid TOML): expected error, got nil")
	}
	if cfg != nil {
		t.Errorf("Load(invalid TOML): expected nil cfg, got %+v", cfg)
	}
}

func TestLoadMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nonexistent.toml")

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
	if !strings.Contains(path, "config.toml") {
		t.Errorf("ConfigPath = %q, want path containing config.toml", path)
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
	if cfg.Embedding.BaseURL != "http://localhost:11434" {
		t.Errorf("Embedding.BaseURL = %q, want http://localhost:11434", cfg.Embedding.BaseURL)
	}
	if cfg.Embedding.Model != "nomic-embed-text" {
		t.Errorf("Embedding.Model = %q, want nomic-embed-text", cfg.Embedding.Model)
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
	if cfg.Search.DefaultTopK != 10 {
		t.Errorf("Search.DefaultTopK = %d, want 10", cfg.Search.DefaultTopK)
	}
	if cfg.Search.Alpha <= 0 {
		t.Errorf("Search.Alpha = %f, want > 0", cfg.Search.Alpha)
	}
}
