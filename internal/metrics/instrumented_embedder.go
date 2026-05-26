// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package metrics

import (
	"context"
	"time"

	"github.com/mattdurham/lth/internal/vector"
)

// InstrumentedEmbedder wraps a vector.Embedder and records request metrics.

// NewInstrumentedEmbedder wraps inner with Prometheus metrics recording.
func NewInstrumentedEmbedder(inner vector.Embedder, provider string, m *Metrics) vector.Embedder {
	return &InstrumentedEmbedder{inner: inner, provider: provider, m: m}
}

// Embed delegates to the inner embedder and records duration and status metrics.
func (ie *InstrumentedEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	start := time.Now()
	result, err := ie.inner.Embed(ctx, text)
	dur := time.Since(start).Seconds()

	status := "success"
	if err != nil {
		status = "error"
	}

	ie.m.EmbedRequestsTotal.WithLabelValues(ie.provider, status).Inc()
	ie.m.EmbedRequestDuration.WithLabelValues(ie.provider).Observe(dur)
	return result, err
}

// Dims delegates to the inner embedder.
func (ie *InstrumentedEmbedder) Dims() int { return ie.inner.Dims() }
