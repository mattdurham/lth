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
The model is configured in `config.toml`.

**Consequence:** Response parsing extracts `choices[0].message.content` as the completion text.
