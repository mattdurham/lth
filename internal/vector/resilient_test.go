// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package vector

import (
	"context"
	"errors"
	"testing"

	"github.com/mattdurham/lth/internal/config"
)

// countingEmbedder returns err on every call and counts how many times Embed was invoked.
type countingEmbedder struct {
	err   error
	calls int
}

func (c *countingEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	c.calls++
	return nil, c.err
}

func (c *countingEmbedder) Dims() int { return 0 }

func TestResilientEmbedder_SkipsRestartOnPayloadTooLarge(t *testing.T) {
	inner := &countingEmbedder{err: ErrPayloadTooLarge}
	cfg := &config.Config{}
	cfg.Embedding.AutoDocker = false // EnsureEmbeddingServer no-ops instantly if it's called
	r := &ResilientEmbedder{inner: inner, cfg: cfg}

	_, err := r.Embed(context.Background(), "some huge content")
	if !errors.Is(err, ErrPayloadTooLarge) {
		t.Errorf("err = %v, want ErrPayloadTooLarge", err)
	}
	if inner.calls != 1 {
		t.Errorf("inner.Embed called %d times, want 1 -- a container restart cannot fix an oversized payload, so the retry must be skipped", inner.calls)
	}
}

func TestResilientEmbedder_RetriesOnTransientError(t *testing.T) {
	inner := &countingEmbedder{err: errors.New("connection refused")}
	cfg := &config.Config{}
	cfg.Embedding.AutoDocker = false
	r := &ResilientEmbedder{inner: inner, cfg: cfg}

	_, err := r.Embed(context.Background(), "some content")
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if inner.calls != 2 {
		t.Errorf("inner.Embed called %d times, want 2 (original + retry-after-restart) for a transient error", inner.calls)
	}
}
