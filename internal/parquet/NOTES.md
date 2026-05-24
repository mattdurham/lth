# internal/parquet — Design Notes

## 1. Embedding Stored as BYTE_ARRAY

*Added: 2026-05-23*

**Decision:** Store Embedding as []byte (BYTE_ARRAY zstd) rather than a Parquet LIST of FLOAT.

**Rationale:** parquet-go LIST schema for float32 slices requires schema-level annotation and is
significantly more complex to read/write correctly. The raw bytes format matches db.MemoryRow.Embedding
exactly, enabling zero-copy conversion. Analytics users can decode the bytes as little-endian float32
arrays in DuckDB or pandas.

**Consequence:** Embedding column is opaque bytes in Parquet tooling unless the caller knows the format.
Document in SPECS.md and any downstream tooling docs.

## 2. io.ReadAll Before GenericReader

*Added: 2026-05-23*

**Decision:** Reader.Read calls io.ReadAll before passing bytes.NewReader to NewGenericReader (which
requires io.ReaderAt for random access).

**Rationale:** BlobStore.Get returns an io.ReadCloser (streaming), not an io.ReaderAt. parquet-go
requires random access (seeks for row group index). Buffering into memory is acceptable because
individual Parquet files are bounded by the push batch size (max 100 MB body → typical Parquet file
well under 50 MB after compression).

**Consequence:** Memory usage per pull request = sum of Parquet file sizes for the requested
layers/date range. Mitigation: pull handler limits total Parquet bytes read per request.
