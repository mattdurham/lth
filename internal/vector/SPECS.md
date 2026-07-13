# internal/vector — Invariants

1. `Cosine(a, b)` returns a value in `[-1.0, 1.0]` for any non-zero float32 vectors.
2. `Cosine(a, a)` returns exactly `1.0` for any non-zero vector `a`.
3. `Cosine` of two zero-length (all-zeros) vectors returns `0.0` — no divide-by-zero panic.
4. `Cosine` of two vectors of different lengths returns `0.0` (length mismatch is not an error).
5. `ToBytes` and `FromBytes` are exact inverses: `FromBytes(ToBytes(v))` equals `v` for any `[]float32`.
6. `ToBytes` encodes float32 values as IEEE 754 little-endian bytes (4 bytes per element).
7. `OllamaEmbedder.Dims()` returns the dimension of the last successful `Embed` call, or 0 if never called.
8. `Embedder.Embed` must not mutate its input string argument.
9. `OllamaEmbedder.Embed` converts the `[]float64` API response to `[]float32` on receipt.
10. `NewEmbedder(cfg)` is the sole constructor for `Embedder` instances.
11. `EnsureEmbeddingServer(cfg)` is a no-op unless `cfg.Embedding.Provider == "huggingface"` AND `cfg.Embedding.AutoDocker == true`.
12. `EnsureEmbeddingServer` returns nil if the server is already reachable; it only starts Docker if the server is unreachable.
13. HuggingFace TEI exposes an OpenAI-compatible `/v1/embeddings` endpoint; `OllamaEmbedder` is used for it.
14. When `cfg.Embedding.AutoDocker == true` and `cfg.Embedding.Provider == "huggingface"`, `NewEmbedder` wraps the inner embedder in a `ResilientEmbedder` that calls `EnsureEmbeddingServer` and retries once on failure.
15. `ResilientEmbedder.Dims()` delegates to the inner embedder.
16. `OllamaEmbedder.Embed` truncates input text to `MaxEmbedInputBytes` (30 KB) at a valid UTF-8 boundary before sending it to the embedding endpoint. This prevents pathologically large memories from infinitely failing against the embedder's token limit and starving the backfill loop. The truncation is logged at debug level.
17. `OllamaEmbedder.Embed` returns `ErrPayloadTooLarge` (checkable via `errors.Is`) specifically when the server responds with HTTP 413, distinct from the generic error returned for any other non-200 status. This can still happen even after invariant 16's truncation, since `MaxEmbedInputBytes` assumes ~4 bytes/token and some content (e.g. dense JSON/libsonnet) tokenizes much more densely.
18. `ResilientEmbedder.Embed` does NOT call `EnsureEmbeddingServer`/retry when the inner error is `ErrPayloadTooLarge` -- a container restart cannot fix an oversized payload, so it returns the error immediately instead of wasting a restart-and-retry round trip.
19. `BackfillEmbeddings` (internal/memory) treats `ErrPayloadTooLarge` as permanent: it soft-deletes the memory (`compacted_at` set) instead of retrying it every batch forever, and calls the caller-supplied `onGiveUp` callback once. Any other embed error is treated as transient and the memory is retried on the next batch.
