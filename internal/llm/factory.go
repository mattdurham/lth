// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package llm

import (
	"fmt"
	"os"
	"time"

	"github.com/mattdurham/lth/internal/config"
	"github.com/mattdurham/lth/internal/llm/anthropicauth"
)

// New returns the configured LLM. If cfg.LLM.Fallbacks is non-empty, the
// returned LLM is a fallback Chain (primary + fallbacks tried in order).
// Otherwise it is a single backend, preserving prior behaviour exactly.
//
// For Anthropic backends with auth_mode="oauth", credentials are loaded
// lazily on first request, so missing credentials surface as a Complete
// error rather than a startup failure.
func New(cfg *config.Config) LLM {
	primary := buildBackend("primary", cfg.LLM.LLMBackend)

	if len(cfg.LLM.Fallbacks) == 0 {
		return primary
	}

	entries := []ChainEntry{{
		Name:          "primary:" + nonEmpty(cfg.LLM.Provider, "anthropic"),
		LLM:           primary,
		Timeout:       backendTimeout(cfg.LLM.LLMBackend),
		MaxConcurrent: cfg.LLM.MaxConcurrent,
	}}
	for i, fb := range cfg.LLM.Fallbacks {
		entries = append(entries, ChainEntry{
			Name:          fmt.Sprintf("fallback%d:%s", i+1, nonEmpty(fb.Provider, "ollama")),
			LLM:           buildBackend(fmt.Sprintf("fallback%d", i+1), fb),
			Timeout:       backendTimeout(fb),
			MaxConcurrent: fb.MaxConcurrent,
		})
	}

	chain := NewChain(ChainConfig{
		CircuitWindow:     cfg.LLM.Chain.CircuitWindow,
		CircuitFailurePct: cfg.LLM.Chain.CircuitFailurePct,
		CircuitCooldown:   time.Duration(cfg.LLM.Chain.CircuitCooldownS) * time.Second,
	}, entries...)
	// Hot-reloadable per-backend timeouts: index 0 = primary, 1+ = fallbacks.
	chain.SetTimeoutLookup(func(i int) time.Duration {
		if i == 0 {
			return backendTimeout(cfg.LLM.LLMBackend)
		}
		j := i - 1
		if j < 0 || j >= len(cfg.LLM.Fallbacks) {
			return 0
		}
		return backendTimeout(cfg.LLM.Fallbacks[j])
	})
	return chain
}

// buildBackend constructs a single LLM client from a backend spec. The label
// is used only for logging.
func buildBackend(_ string, b config.LLMBackend) LLM {
	switch b.Provider {
	case "anthropic":
		a := NewAnthropicLLM(b.APIKey, b.Model, b.TimeoutS)
		if b.AuthMode == "oauth" {
			path := b.OAuthCredentialsPath
			if path == "" {
				if p, err := anthropicauth.DefaultPath(); err == nil {
					path = p
				}
			}
			a.SetTokenSource(anthropicauth.NewTokenSource(path))
		}
		return a
	default: // "openai", "ollama", "openai-compat", anything else
		key := b.APIKey
		if key == "" && b.APIKeyEnv != "" {
			key = os.Getenv(b.APIKeyEnv)
		}
		return NewOllamaLLM(b.BaseURL, b.Model, key, b.TimeoutS)
	}
}

// backendTimeout converts the configured timeout_s into a duration used by
// the chain to bound per-backend attempts. Zero means no extra bound (the
// LLM's own http client timeout still applies).
func backendTimeout(b config.LLMBackend) time.Duration {
	if b.TimeoutS <= 0 {
		return 0
	}
	return time.Duration(b.TimeoutS) * time.Second
}

func nonEmpty(s, dflt string) string {
	if s == "" {
		return dflt
	}
	return s
}
