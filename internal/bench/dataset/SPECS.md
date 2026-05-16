# dataset — Specifications

## Invariants

1. `Problem.Language()` returns the language by repo-name lookup; it never makes a network call.
2. `repoLanguage` is the sole source of truth for language detection — no external map or config.
3. `HFClient.FetchProblems` always applies the language filter before returning; callers receive only matching problems.
4. `FailToPass` and `PassToPass` unmarshal as `[]string` (native JSON arrays in the API response — not double-encoded).
5. `HFClient.baseURL` is overridable for testing; production code must use the default HuggingFace URL.
