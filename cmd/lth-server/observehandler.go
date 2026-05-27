package main

import (
	"github.com/mattdurham/lth/internal/blobstore"
	"github.com/mattdurham/lth/internal/parquet"
)

type ObserveHandler struct {
	store  blobstore.BlobStore
	writer *parquet.Writer
}
