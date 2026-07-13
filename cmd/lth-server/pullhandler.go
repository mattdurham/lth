// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package main

import (
	"github.com/mattdurham/lth/internal/blobstore"
	"github.com/mattdurham/lth/internal/parquet"
)

type PullHandler struct {
	store  blobstore.BlobStore
	reader *parquet.Reader
}
