// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package blobstore

import "github.com/minio/minio-go/v7"

type S3Store struct {
	client *minio.Client
	bucket string
}
