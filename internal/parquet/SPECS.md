# internal/parquet — Invariants

1. MemoryRecord is the canonical Parquet schema; schema changes require a NOTES.md entry.
2. Writer.Write with an empty or nil slice produces a valid Parquet file with zero rows (not an error).
3. Reader.Read with a zero since (time.Time{}) returns all records in the file.
4. Reader.Read filters by CreatedAt >= since (inclusive). UpdatedAt is not used for filtering.
5. Embedding is stored as raw IEEE 754 little-endian float32 bytes ([]byte BYTE_ARRAY), not as a Parquet LIST of FLOAT.
6. Parquet files are compressed with zstd. No other compression codec is supported.
7. The parquet package is write-once from the server perspective; Parquet files are never modified after creation.
8. Reader.Read always returns a non-nil slice.
