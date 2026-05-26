// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package metrics

import (
	"context"
	"time"

	"github.com/mattdurham/lth/internal/llm"
)

// InstrumentedLLM wraps an llm.LLM and records request metrics.

// NewInstrumentedLLM wraps inner with Prometheus metrics recording.
func NewInstrumentedLLM(inner llm.LLM, provider string, m *Metrics) llm.LLM {
	return &InstrumentedLLM{inner: inner, provider: provider, m: m}
}

// Complete delegates to the inner LLM and records duration and status metrics.
func (il *InstrumentedLLM) Complete(ctx context.Context, prompt string) (string, error) {
	start := time.Now()
	result, err := il.inner.Complete(ctx, prompt)
	dur := time.Since(start).Seconds()

	status := "success"
	if err != nil {
		status = "error"
	}

	il.m.LLMRequestsTotal.WithLabelValues(il.provider, "complete", status).Inc()
	il.m.LLMRequestDuration.WithLabelValues(il.provider, "complete").Observe(dur)
	return result, err
}
