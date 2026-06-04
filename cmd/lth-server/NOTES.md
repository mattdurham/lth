# cmd/lth-server — Design Notes

## 1. Pull Path: BlobStore → Parquet → ZIP+JSONL

*Added: 2026-05-23*

**Decision:** The pull handler reads Parquet files from BlobStore (not SQLite) to produce ZIP+JSONL responses.

**Rationale:** The task specification requires "Pull reads Parquet from S3". The pull handler lists BlobStore
objects by prefix + date filter, reads each Parquet file, applies the since filter, and assembles the ZIP.

**Consequence:** Push writes to BlobStore Parquet only (no SQLite on server). Pull reads only BlobStore.
If BlobStore objects are lost, pull will return empty results. Operators must back up BlobStore.

## 2. No SQLite on Server

*Added: 2026-05-23*

**Decision:** lth-server has no SQLite database. Push writes Parquet directly to BlobStore.

**Rationale:** The brainstorm suggested SQLite for dedup, but the task spec clarifies the server is a
stateless storage layer. Parquet-based storage avoids the SQLite CGO requirement on server deployments.

**Consequence:** No content_hash dedup on push — the same memory may be stored multiple times if pushed
multiple times. Dedup is the client responsibility (lth sync push filters source=server memories).

## 3. 100 MB Max Request Body

*Added: 2026-05-23*

**Decision:** Wrap the HTTP mux with `http.MaxBytesHandler(mux, 100*1024*1024)`.

**Rationale:** Large push payloads could exhaust server memory without a limit. 100 MB accommodates
~50,000 text-only memories or ~5,000 memories with 768-dim embeddings.

**Consequence:** Clients with more than ~50,000 new memories per sync must chunk their pushes.

## 4. YAML Config, Not TOML

*Added: 2026-05-23*

**Decision:** lth-server uses gopkg.in/yaml.v3 for its config file, not the TOML used by lth.

**Rationale:** The task specification requires YAML for lth-server. lth-server is a distinct binary
deployed server-side. YAML is more familiar to operators configuring servers.

**Consequence:** lth-server.yaml is separate from ~/.lth/config.yaml. The two configs cannot be merged.
