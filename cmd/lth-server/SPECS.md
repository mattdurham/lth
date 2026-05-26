# cmd/lth-server — Invariants

1. Identity is established exclusively via HTTP headers: X-LTH-Account, X-LTH-Org, X-LTH-User (required), X-LTH-Team (optional). No bearer tokens or other auth mechanisms.
2. POST /v1/sync/push accepts a ZIP+JSONL body in the same format as `lth export` output. Max body size: 100 MB.
3. GET /v1/sync/pull returns a ZIP+JSONL body in the same format as `lth export` output. All returned memories have source="server".
4. POST /v1/observations accepts NDJSON body. L5 observations are written to BlobStore only; no SQLite insert.
5. L5 has no pull endpoint. GET /v1/sync/pull with layers=5 returns 400 Bad Request.
6. Push skips memories with source="server" to prevent sync loops. All other memories are accepted and written to BlobStore.
7. Server never calls any LLM. No embedding generation, no compaction, no scoring.
8. Pull reads Parquet files from BlobStore filtered by prefix (layer/date) and since timestamp.
9. BlobStore key format: {account}/{org}/{scope}/{layer}/date={date}/{id}.parquet where scope is users/{user} for L1/L2/L5, shared for L3, teams/{team} or shared for L4.
10. The server YAML config is separate from the lth TOML config. Server config file: lth-server.yaml (default).
11. NOTE invariant on all .go files. One public struct per file.
12. The server has no SQLite database. Push writes Parquet directly to BlobStore. Pull reads Parquet from BlobStore.
