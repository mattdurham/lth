// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package vector

import "github.com/mattdurham/lth/internal/config"

// NewEmbedder creates the configured Embedder. Call EnsureEmbeddingServer first when
// using the "huggingface" provider to ensure the server is available.
//
// HuggingFace TEI and Ollama both expose an OpenAI-compatible /v1/embeddings endpoint,
// so OllamaEmbedder handles both — they differ only in base URL and model name.
//
// When AutoDocker is enabled the returned embedder is wrapped in a ResilientEmbedder
// that restarts the container and retries once on failure.
func NewEmbedder(cfg *config.Config) Embedder {
	inner := NewOllamaEmbedder(cfg.Embedding.BaseURL, config.EmbeddingModel, cfg.Embedding.TimeoutS)
	if cfg.Embedding.AutoDocker && cfg.Embedding.Provider == "huggingface" {
		return &ResilientEmbedder{inner: inner, cfg: cfg}
	}
	return inner
}
