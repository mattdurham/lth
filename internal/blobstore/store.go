// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package blobstore

// BlobObject describes a single object in a BlobStore.

// BlobStore is a content-addressed object store for Parquet and index blobs.

// Put writes r to the given key, replacing any existing object.

// Get returns a ReadCloser for the object at key.
// Returns a wrapped fs.ErrNotExist if the key does not exist.

// Exists returns true if key exists, false if not. Never returns fs.ErrNotExist.

// Delete removes the object at key. No error if key does not exist.

// List returns all objects whose keys begin with prefix.
// An empty prefix returns all objects.
