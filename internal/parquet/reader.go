// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package parquet

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"time"

	goparquet "github.com/parquet-go/parquet-go"
)

// Reader reads MemoryRecord slices from Parquet data.
type Reader struct{}

// NewReader returns a new Reader.
func NewReader() *Reader { return &Reader{} }

// Read deserializes Parquet data from r, filtering records where CreatedAt >= since.
// A zero since (time.Time{}) returns all records.
// Returns a non-nil empty slice if no records match.
func (rd *Reader) Read(ctx context.Context, r io.Reader, since time.Time) ([]MemoryRecord, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("read parquet data: %w", err)
	}
	br := bytes.NewReader(data)
	pr := goparquet.NewGenericReader[MemoryRecord](br)
	defer pr.Close()

	results := make([]MemoryRecord, 0)
	buf := make([]MemoryRecord, 512)
	for {
		n, readErr := pr.Read(buf)
		for i := 0; i < n; i++ {
			if since.IsZero() || !buf[i].CreatedAt.Before(since) {
				results = append(results, buf[i])
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, fmt.Errorf("read parquet rows: %w", readErr)
		}
	}
	return results, nil
}
