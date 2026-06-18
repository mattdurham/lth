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
11. When a chain `ChainEntry` has `MaxConcurrent > 0`, the chain wraps that backend in a buffered-channel semaphore of that capacity. Concurrent `Chain.Complete` callers acquire a slot before invoking the underlying client and release on return; callers beyond the cap wait on the channel bounded by the entry's `Timeout`. A semaphore-wait timeout is treated as congestion (not a backend fault): the breaker is NOT incremented, and the chain falls through to the next backend.
12. `MaxConcurrent == 0` (default) preserves the pre-semaphore behaviour: unbounded concurrent calls to that backend.
13. When `Chain.SetTimeoutLookup(f)` has been called and `f(i)` returns a non-zero `time.Duration` for backend index `i`, that value overrides `ChainEntry.Timeout` for that call. Lets per-backend timeouts hot-reload from a live config pointer without rebuilding the chain. `f(i) == 0` falls back to the static `Timeout`.
