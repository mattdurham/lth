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

---

## 4. Anthropic OAuth (claude.ai Pro/Max) Support

*Added: 2026-06-10*

**Decision:** Add an opt-in OAuth 2.0 PKCE flow (in `internal/llm/anthropicauth`) that
lets users authenticate against an Anthropic Pro/Max subscription instead of an
API key. When `cfg.LLM.AuthMode == "oauth"`, the `AnthropicLLM` swaps `x-api-key`
for `Authorization: Bearer <token>` plus the Claude Code identity headers
(`anthropic-beta: claude-code-20250219,oauth-2025-04-20`, `user-agent: claude-cli/...`,
`x-app: cli`).

**Rationale:** Mirrors what Claude Code and `~/source/pi` do. Without the
identity headers, Anthropic rejects OAuth tokens on the Messages API. Keeping
the x-api-key path as the default preserves backwards compatibility — no
existing config breaks.

**Consequence:**
- New `lth auth {login,logout,status}` commands manage credentials at
  `~/.lth/anthropic-oauth.json` (mode 0600).
- The OAuth client ID is the same public ID Claude Code uses
  (`9d1c250a-e61b-44d9-88ed-5944d1962f5e`) so tokens are accepted by Anthropic's
  OAuth-gated path.
- `TokenSource` refreshes expired tokens transparently on the next request,
  persisting the rotated refresh token back to disk under a mutex.
- Use of claude.ai OAuth tokens from a non-Anthropic tool is a gray area
  w.r.t. Anthropic's ToS; users opt in explicitly via `auth_mode: oauth`.

---

## 5. Client-Side Admission Control for Concurrency-Limited Backends

*Added: 2026-06-17*

**Decision:** Add `MaxConcurrent` to each chain `ChainEntry`. When set,
`Chain.Complete` acquires a slot in a buffered channel semaphore before
invoking that backend's underlying client; callers beyond the cap wait on
the channel, bounded by the entry's `Timeout`.

**Rationale:** Background workers (tags / valence / importance backfill,
mdwatcher fact extraction, issueswatcher, gwswatcher) fire LLM calls from
independent goroutines, all sharing the same `Chain.Complete`. A consumer-
hardware local model (e.g. Qwen3-4B in LM Studio) serves one inference at a
time; multiple concurrent client requests pile up inside the inference
server's own queue, where the wait time is invisible to the chain and
often exceeds the per-backend `Timeout`. The result was a flapping
circuit breaker, mass fallback to the cloud backend, and elevated cost
for no quality benefit.

Client-side semaphores let lth admit just enough concurrent calls for what
the backend can actually handle (typically 1 for a single-GPU local model),
so each in-flight call sees an idle server and returns promptly. Excess
callers wait their turn rather than racing into timeouts.

**Consequence:**
- Semaphore-wait timeout is treated as congestion, not a fault: the breaker
  is not incremented and the chain falls through to the next backend. This
  preserves smooth degradation under bursts without thrashing the breaker.
- `MaxConcurrent == 0` keeps the pre-semaphore unbounded behaviour, so the
  change is backwards compatible.
- Recommended pattern: set primary `max_concurrent: 1` for a serial local
  model and leave the cloud fallback at `0` (unbounded).
