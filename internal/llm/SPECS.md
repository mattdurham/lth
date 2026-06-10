# internal/llm — Invariants

1. The `LLM` interface is the sole API; callers never construct `OllamaLLM` or `AnthropicLLM` directly — they use `New(cfg)`.
2. `Complete` always respects the context deadline — it never blocks beyond the context timeout.
3. `Complete` returns an error for any non-200 HTTP response.
4. `Complete` never panics; all errors are returned.
5. `OllamaLLM` uses the OpenAI-compatible `/v1/chat/completions` endpoint.
6. `AnthropicLLM` uses the Anthropic Messages API (`/v1/messages`) with `anthropic-version: 2023-06-01`.
7. `New(cfg)` is the sole constructor; it selects the implementation based on `cfg.LLM.Provider`.
8. If `AnthropicLLM.apiKey` is empty, `ANTHROPIC_API_KEY` environment variable is used at call time.
9. If `AnthropicLLM.tokenSource` is non-nil, requests use `Authorization: Bearer <token>` together with the `anthropic-beta: claude-code-20250219,oauth-2025-04-20`, `user-agent: claude-cli/...`, and `x-app: cli` headers; `apiKey`/`ANTHROPIC_API_KEY` are ignored.
10. `New(cfg)` configures an OAuth `TokenSource` when `cfg.LLM.Provider == "anthropic" && cfg.LLM.AuthMode == "oauth"`. Credentials are loaded lazily on first request, not at construction, so missing credentials surface as a `Complete` error rather than a startup panic.
