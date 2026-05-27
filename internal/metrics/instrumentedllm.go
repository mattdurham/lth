package metrics

import "github.com/mattdurham/lth/internal/llm"

type InstrumentedLLM struct {
	inner    llm.LLM
	provider string
	m        *Metrics
}
