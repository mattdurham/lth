// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package parquet

import (
	"context"
	"fmt"
	"io"

	goparquet "github.com/parquet-go/parquet-go"
)

// Writer writes MemoryRecord slices as Parquet to an io.Writer.
type Writer struct{}

// NewWriter returns a new Writer.
func NewWriter() *Writer { return &Writer{} }

// Write serializes records to Parquet format into w.
// An empty records slice produces a valid Parquet file with zero rows.
func (wr *Writer) Write(ctx context.Context, w io.Writer, records []MemoryRecord) error {
	pw := goparquet.NewGenericWriter[MemoryRecord](w,
		goparquet.Compression(&goparquet.Zstd),
	)
	if len(records) > 0 {
		if _, err := pw.Write(records); err != nil {
			return fmt.Errorf("parquet write: %w", err)
		}
	}
	if err := pw.Close(); err != nil {
		return fmt.Errorf("parquet close: %w", err)
	}
	return nil
}
