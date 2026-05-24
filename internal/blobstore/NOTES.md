# internal/blobstore — Design Notes

## 1. minio-go for S3-Compatible Object Store

*Added: 2026-05-23*

**Decision:** Use `github.com/minio/minio-go/v7` for S3-compatible object storage.

**Rationale:** minio-go is ~3x lighter in dep graph than aws-sdk-go-v2, handles AWS S3, MinIO,
Cloudflare R2, and GCS S3 compatibility, and is Apache 2.0. The project must remain CGO-free;
minio-go is pure Go.

**Consequence:** S3Store configuration uses minio-go credential model directly. AWS STS/IAM
role-based auth is not supported in v1 — only static access key + secret.

## 2. LocalStore Atomic Writes

*Added: 2026-05-23*

**Decision:** LocalStore.Put writes to a temporary file in the same directory as the target,
then renames. Temp file name: `.<uuid>.tmp`.

**Rationale:** os.Rename is atomic on Linux (same filesystem). Without this, a crash mid-write
leaves a corrupt partial file at the key path.

**Consequence:** The directory containing the target file must be writable. The base directory
must be on a single filesystem for rename atomicity.
