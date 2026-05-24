// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package blobstore

import (
	"context"
	"io"
	"time"
)

// BlobObject describes a single object in a BlobStore.
type BlobObject struct {
	Key          string
	Size         int64
	LastModified time.Time
}

// BlobStore is a content-addressed object store for Parquet and index blobs.
type BlobStore interface {
	// Put writes r to the given key, replacing any existing object.
	Put(ctx context.Context, key string, r io.Reader) error
	// Get returns a ReadCloser for the object at key.
	// Returns a wrapped fs.ErrNotExist if the key does not exist.
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	// Exists returns true if key exists, false if not. Never returns fs.ErrNotExist.
	Exists(ctx context.Context, key string) (bool, error)
	// Delete removes the object at key. No error if key does not exist.
	Delete(ctx context.Context, key string) error
	// List returns all objects whose keys begin with prefix.
	// An empty prefix returns all objects.
	List(ctx context.Context, prefix string) ([]BlobObject, error)
}
