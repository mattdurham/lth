// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package blobstore

import "time"

type BlobObject struct {
	Key          string
	Size         int64
	LastModified time.Time
}
