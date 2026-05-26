package blobstore

import "github.com/minio/minio-go/v7"

type S3Store struct {
	client *minio.Client
	bucket string
}
