// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package llm

import "github.com/mattdurham/lth/internal/config"

// New returns the configured LLM implementation based on cfg.LLM.Provider.
// It is the sole entry point for creating LLM instances.
func New(cfg *config.Config) LLM {
	switch cfg.LLM.Provider {
	case "anthropic":
		return NewAnthropicLLM(cfg.LLM.APIKey, cfg.LLM.Model, cfg.LLM.TimeoutS)
	default: // "ollama", "openai", or anything else
		return NewOllamaLLM(cfg.LLM.BaseURL, cfg.LLM.Model, cfg.LLM.TimeoutS)
	}
}
