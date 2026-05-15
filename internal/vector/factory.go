// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package vector

import "github.com/mattdurham/lth/internal/config"

// NewEmbedder creates the configured Embedder. Call EnsureEmbeddingServer first when
// using the "huggingface" provider to ensure the server is available.
//
// HuggingFace TEI and Ollama both expose an OpenAI-compatible /v1/embeddings endpoint,
// so OllamaEmbedder handles both — they differ only in base URL and model name.
func NewEmbedder(cfg *config.Config) Embedder {
	return NewOllamaEmbedder(cfg.Embedding.BaseURL, cfg.Embedding.Model, cfg.Embedding.TimeoutS)
}
