package blobstore

import (
	"context"
	"io"
)

type BlobStore interface {
	Put(ctx context.Context, key string, r io.Reader) error
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	Exists(ctx context.Context, key string) (bool, error)
	Delete(ctx context.Context, key string) error
	List(ctx context.Context, prefix string) ([]BlobObject, error)
}
