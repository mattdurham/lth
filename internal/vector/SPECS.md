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
