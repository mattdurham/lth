// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// Package llm provides the LLM interface and OpenAI-compatible implementation.
package llm

import "context"

// LLM is the interface for large language model completions.
// It is used by both internal/memory (importance scoring) and internal/compactor (summarization).
type LLM interface {
	// Complete sends a prompt to the LLM and returns the text response.
	// It respects context cancellation and deadline.
	Complete(ctx context.Context, prompt string) (string, error)
}
