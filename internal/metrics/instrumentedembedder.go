// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package metrics

import "github.com/mattdurham/lth/internal/vector"

type InstrumentedEmbedder struct {
	inner    vector.Embedder
	provider string
	m        *Metrics
}
