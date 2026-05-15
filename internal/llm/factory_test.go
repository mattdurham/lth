// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package llm

import (
	"testing"

	"github.com/mattdurham/lth/internal/config"
)

func TestNew_anthropicProvider(t *testing.T) {
	cfg := &config.Config{}
	cfg.LLM.Provider = "anthropic"
	cfg.LLM.APIKey = "test-key"
	cfg.LLM.Model = "claude-haiku-4-5-20251001"
	cfg.LLM.TimeoutS = 30

	l := New(cfg)
	if _, ok := l.(*AnthropicLLM); !ok {
		t.Errorf("New with provider=anthropic returned %T, want *AnthropicLLM", l)
	}
}

func TestNew_ollamaProvider(t *testing.T) {
	cfg := &config.Config{}
	cfg.LLM.Provider = "ollama"
	cfg.LLM.BaseURL = "http://localhost:11434"
	cfg.LLM.Model = "llama3.2"
	cfg.LLM.TimeoutS = 60

	l := New(cfg)
	if _, ok := l.(*OllamaLLM); !ok {
		t.Errorf("New with provider=ollama returned %T, want *OllamaLLM", l)
	}
}

func TestNew_unknownProviderFallsBackToOllama(t *testing.T) {
	cfg := &config.Config{}
	cfg.LLM.Provider = "unknown-provider"
	cfg.LLM.BaseURL = "http://localhost:11434"
	cfg.LLM.Model = "llama3.2"
	cfg.LLM.TimeoutS = 60

	l := New(cfg)
	if _, ok := l.(*OllamaLLM); !ok {
		t.Errorf("New with unknown provider returned %T, want *OllamaLLM", l)
	}
}

func TestNew_openaiProvider(t *testing.T) {
	cfg := &config.Config{}
	cfg.LLM.Provider = "openai"
	cfg.LLM.BaseURL = "https://api.openai.com"
	cfg.LLM.Model = "gpt-4o"
	cfg.LLM.TimeoutS = 60

	l := New(cfg)
	if _, ok := l.(*OllamaLLM); !ok {
		t.Errorf("New with provider=openai returned %T, want *OllamaLLM", l)
	}
}
