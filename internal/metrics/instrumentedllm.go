// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package metrics

import "github.com/mattdurham/lth/internal/llm"

type InstrumentedLLM struct {
	inner    llm.LLM
	provider string
	m        *Metrics
}
