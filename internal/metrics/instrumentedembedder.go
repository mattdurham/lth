package metrics

import "github.com/mattdurham/lth/internal/vector"

type InstrumentedEmbedder struct {
	inner    vector.Embedder
	provider string
	m        *Metrics
}
