# internal/vector — Design Notes

## 1. float32 for Embedding Storage

*Added: 2026-05-14*

**Decision:** Use `float32` (not `float64`) for all embedding vectors in memory and on disk.

**Rationale:** Embedding models produce float32 outputs. Storing as float64 would double the
storage cost with no accuracy benefit. The OpenAI API response contains float64 values — these
are downcast to float32 on receipt, which is the standard approach for embedding pipelines.

**Consequence:** A 768-dimension embedding occupies 3072 bytes (768 × 4). Some precision is
lost when downcasting from float64, but this is negligible for cosine similarity computation.

## 2. Pure-Go Cosine Similarity

*Added: 2026-05-14*

**Decision:** Implement cosine similarity as a pure Go function without SIMD or assembly.

**Rationale:** For the search path (ranking top-K results), cosine similarity is computed on
at most a few hundred candidates. At 768 dimensions, 200 candidates = 153,600 float32 multiplications
— microseconds in Go. The KNN bottleneck (searching all memories) is handled by sqlite-vec vec0,
not by this function.

**Consequence:** The cosine function is used only for: (1) exact rescoring of vec0 KNN results,
(2) Zettelkasten auto-link threshold check, (3) L4→L3 compaction clustering. All of these are
low-frequency or bounded-N operations where SIMD would provide negligible benefit.

## 3. OpenAI-Compatible Embedding Endpoint

*Added: 2026-05-14*

**Decision:** The `OllamaEmbedder` uses the OpenAI-compatible `/v1/embeddings` endpoint.

**Rationale:** Ollama exposes an OpenAI-compatible API, so the same implementation works with
Ollama (local), OpenAI, and any compatible proxy. The request format is:
`POST /v1/embeddings` with `{"model": "<model>", "input": "<text>"}`.

**Consequence:** Users can switch from Ollama to any OpenAI-compatible embedding service by
changing `base_url` and `model` in the config.

---

## 4. HuggingFace TEI Provider and Auto-Docker

*Added: 2026-05-14*

**Decision:** Add HuggingFace TEI as the default embedding provider, with `EnsureEmbeddingServer`
auto-starting a Docker container when the server is unreachable, and `NewEmbedder(cfg)` as the
sole factory for `Embedder` instances.

**Rationale:** HuggingFace TEI (`ghcr.io/huggingface/text-embeddings-inference:cpu-1.5`) provides
high-quality local embeddings without requiring Ollama. It exposes an OpenAI-compatible
`/v1/embeddings` endpoint, so `OllamaEmbedder` handles it without code changes. Auto-Docker
removes the manual setup step for new users while remaining opt-out via `auto_docker = false`.

**Consequence:** First startup with `auto_docker = true` may take up to 90 seconds if the model
needs to be downloaded. The Docker container is named `lth-embeddings` for lifecycle management.
A `docker-compose.yml` is provided for users who prefer explicit management over auto-docker.

---

## 5. ResilientEmbedder — Restart-on-Failure Wrapper

*Added: 2026-06-03*

**Decision:** Wrap `OllamaEmbedder` in a `ResilientEmbedder` (when `auto_docker = true`) that calls
`EnsureEmbeddingServer` and retries once on any `Embed` failure.

**Rationale:** `EnsureEmbeddingServer` is called only at startup. If the Docker container exits
while lth is running (e.g. SIGTERM from the host), subsequent embed calls fail silently — new
memories are written to the DB but never vectorized. The wrapper closes this gap without requiring
a separate health-check goroutine or a restart policy on the container.

**Consequence:** A failing embed now incurs one extra `docker start` attempt and one retry before
returning an error. The retry adds latency only on failure paths; the happy path is unchanged.
