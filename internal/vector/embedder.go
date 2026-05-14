// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// Package vector provides the Embedder interface, cosine similarity, and float32 serialization.
package vector

import "context"

// Embedder generates float32 embedding vectors from text.
type Embedder interface {
	// Embed returns the embedding vector for the given text.
	// It must not mutate the input string.
	Embed(ctx context.Context, text string) ([]float32, error)

	// Dims returns the dimension of the last successful Embed call.
	// Returns 0 if Embed has never been called successfully.
	Dims() int
}
