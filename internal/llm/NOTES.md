# internal/llm — Design Notes

## 1. Shared LLM Interface

*Added: 2026-05-14*

**Decision:** Define `LLM` interface in `internal/llm` instead of duplicating it in both
`internal/memory` and `internal/compactor`.

**Rationale:** Both packages need an identical `Complete(ctx, prompt) (string, error)` interface.
Defining it once in `internal/llm` avoids duplication and allows a single concrete implementation.

**Consequence:** Both `internal/memory` and `internal/compactor` import `internal/llm`.
This does not create a cycle since `internal/llm` has no project imports.

---

## 2. OpenAI-Compatible Chat Completions Endpoint

*Added: 2026-05-14*

**Decision:** Use `/v1/chat/completions` with a single user message for LLM calls.

**Rationale:** Same as the embedding endpoint — Ollama and OpenAI both support this format.
The model is configured in `config.yaml`.

**Consequence:** Response parsing extracts `choices[0].message.content` as the completion text.

---

## 3. Anthropic Provider and Factory

*Added: 2026-05-14*

**Decision:** Add `AnthropicLLM` implementing the Anthropic Messages API and a `New(cfg)` factory
function as the sole entry point for creating LLM instances.

**Rationale:** The default LLM provider for lth is Anthropic (claude-haiku-4-5-20251001).
Anthropic's Messages API differs from the OpenAI chat completions format: it uses `x-api-key`
headers, `anthropic-version`, a `content` array in the response, and max_tokens is required.
The factory pattern ensures callers do not need to know which concrete type to instantiate.

**Consequence:** `cfg.LLM.Provider` selects the implementation: "anthropic" → `AnthropicLLM`,
anything else → `OllamaLLM`. The API key can be set in config or via `ANTHROPIC_API_KEY` env var.
