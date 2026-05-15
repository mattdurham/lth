// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package vector

import (
	"testing"

	"github.com/mattdurham/lth/internal/config"
)

func TestNewEmbedder_returnsOllamaEmbedder(t *testing.T) {
	cfg := &config.Config{}
	cfg.Embedding.Provider = "huggingface"
	cfg.Embedding.BaseURL = "http://localhost:8080"
	cfg.Embedding.Model = "nomic-ai/nomic-embed-text-v1.5"
	cfg.Embedding.TimeoutS = 30

	emb := NewEmbedder(cfg)
	if _, ok := emb.(*OllamaEmbedder); !ok {
		t.Errorf("NewEmbedder returned %T, want *OllamaEmbedder", emb)
	}
}

func TestNewEmbedder_ollamaProvider(t *testing.T) {
	cfg := &config.Config{}
	cfg.Embedding.Provider = "ollama"
	cfg.Embedding.BaseURL = "http://localhost:11434"
	cfg.Embedding.Model = "nomic-embed-text"
	cfg.Embedding.TimeoutS = 30

	emb := NewEmbedder(cfg)
	if emb == nil {
		t.Fatal("NewEmbedder returned nil")
	}
}
