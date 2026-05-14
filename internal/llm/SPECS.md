# internal/llm — Invariants

1. The `LLM` interface is the sole API; callers never construct `OllamaLLM` directly (they use `NewOllamaLLM`).
2. `Complete` always respects the context deadline — it never blocks beyond the context timeout.
3. `Complete` returns an error for any non-200 HTTP response.
4. `Complete` never panics; all errors are returned.
5. The implementation uses the OpenAI-compatible `/v1/chat/completions` endpoint.
