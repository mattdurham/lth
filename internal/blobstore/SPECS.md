# internal/blobstore — Invariants

1. BlobStore is the sole public interface; callers must not depend on LocalStore or S3Store directly.
2. Get returns a wrapped fs.ErrNotExist (errors.Is(err, fs.ErrNotExist) == true) when key is absent.
3. Put is atomic for LocalStore: write goes to a temp file, then os.Rename. Partial writes are never visible.
4. Keys use "/" as the logical separator regardless of OS. LocalStore maps "/" to filepath.Separator internally.
5. LocalStore rejects keys that escape the base directory (path traversal). Put/Get/Exists/Delete return error if the resolved path is outside baseDir.
6. List returns an empty slice (not nil) when no keys match the prefix.
7. S3Store maps all errors to the BlobStore contract: only fs.ErrNotExist for missing keys; all other errors propagate wrapped.
8. NewS3Store calls BucketExists on construction and returns an error if the bucket is not accessible.
