package blobstore

import "time"

type BlobObject struct {
	Key          string
	Size         int64
	LastModified time.Time
}
