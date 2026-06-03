// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package vector

import (
	"context"
	"fmt"

	"github.com/mattdurham/lth/internal/config"
)

// ResilientEmbedder wraps an Embedder and restarts the Docker container on failure before retrying.
// It is a no-op wrapper when AutoDocker is false or provider is not "huggingface".
type ResilientEmbedder struct {
	inner Embedder
	cfg   *config.Config
}

// Embed calls the inner embedder. On failure it calls EnsureEmbeddingServer and retries once.
func (r *ResilientEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	v, err := r.inner.Embed(ctx, text)
	if err == nil {
		return v, nil
	}

	if restartErr := EnsureEmbeddingServer(r.cfg); restartErr != nil {
		return nil, fmt.Errorf("embed failed and container restart failed: %w (original: %v)", restartErr, err)
	}

	return r.inner.Embed(ctx, text)
}

// Dims delegates to the inner embedder.
func (r *ResilientEmbedder) Dims() int { return r.inner.Dims() }
