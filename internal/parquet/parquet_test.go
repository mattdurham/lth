// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package parquet_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/mattdurham/lth/internal/parquet"
)

func writeRead(t *testing.T, records []parquet.MemoryRecord, since time.Time) []parquet.MemoryRecord {
	t.Helper()
	ctx := context.Background()
	w := parquet.NewWriter()
	var buf bytes.Buffer
	if err := w.Write(ctx, &buf, records); err != nil {
		t.Fatalf("Write: %v", err)
	}
	r := parquet.NewReader()
	got, err := r.Read(ctx, &buf, since)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	return got
}

func makeRecord(id string, createdAt time.Time) parquet.MemoryRecord {
	return parquet.MemoryRecord{
		ID:          id,
		Layer:       1,
		Content:     "content " + id,
		ContentHash: "hash" + id,
		CreatedAt:   createdAt,
		UpdatedAt:   createdAt,
		Source:      "test",
	}
}

func TestWrite_EmptySlice(t *testing.T) {
	ctx := context.Background()
	w := parquet.NewWriter()
	var buf bytes.Buffer
	if err := w.Write(ctx, &buf, nil); err != nil {
		t.Fatalf("Write empty: %v", err)
	}
	if buf.Len() == 0 {
		t.Error("expected non-empty parquet bytes even for empty slice")
	}
}

func TestReadWrite_RoundTrip(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	records := make([]parquet.MemoryRecord, 5)
	for i := range records {
		records[i] = makeRecord(fmt.Sprintf("%d", i), base.Add(time.Duration(i)*time.Hour))
	}
	got := writeRead(t, records, time.Time{})
	if len(got) != 5 {
		t.Fatalf("got %d records want 5", len(got))
	}
	for i, r := range got {
		if r.ID != records[i].ID {
			t.Errorf("record %d ID: got %q want %q", i, r.ID, records[i].ID)
		}
		if r.Content != records[i].Content {
			t.Errorf("record %d Content: got %q want %q", i, r.Content, records[i].Content)
		}
	}
}

func TestReadWrite_SinceFilter(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var records []parquet.MemoryRecord
	for i := 0; i < 5; i++ {
		records = append(records, makeRecord(fmt.Sprintf("%d", i), base.Add(time.Duration(i)*time.Hour)))
	}
	since := base.Add(2 * time.Hour)
	got := writeRead(t, records, since)
	if len(got) != 3 {
		t.Fatalf("got %d records want 3 (since T+2h)", len(got))
	}
}

func TestReadWrite_SinceFilter_All(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var records []parquet.MemoryRecord
	for i := 0; i < 5; i++ {
		records = append(records, makeRecord(fmt.Sprintf("%d", i), base.Add(time.Duration(i)*time.Hour)))
	}
	got := writeRead(t, records, time.Time{})
	if len(got) != 5 {
		t.Fatalf("got %d records want 5 (zero since)", len(got))
	}
}

func TestReadWrite_Embedding(t *testing.T) {
	floats := make([]float32, 384)
	for i := range floats {
		floats[i] = float32(i) * 0.001
	}
	b := make([]byte, len(floats)*4)
	for i, f := range floats {
		binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(f))
	}

	rec := makeRecord("emb", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	rec.Embedding = b

	got := writeRead(t, []parquet.MemoryRecord{rec}, time.Time{})
	if len(got) != 1 {
		t.Fatalf("got %d records", len(got))
	}
	if !bytes.Equal(got[0].Embedding, b) {
		t.Error("embedding mismatch")
	}
}

func TestReadWrite_NilEmbedding(t *testing.T) {
	rec := makeRecord("nonemb", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	rec.Embedding = nil

	got := writeRead(t, []parquet.MemoryRecord{rec}, time.Time{})
	if len(got) != 1 {
		t.Fatalf("got %d records", len(got))
	}
	if len(got[0].Embedding) != 0 {
		t.Errorf("expected empty embedding, got %d bytes", len(got[0].Embedding))
	}
}

func TestReadWrite_SpecialChars(t *testing.T) {
	rec := makeRecord("special", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	rec.Content = "line1\nline2\t\"quoted\" unicode: 中文"

	got := writeRead(t, []parquet.MemoryRecord{rec}, time.Time{})
	if len(got) != 1 {
		t.Fatalf("got %d records", len(got))
	}
	if got[0].Content != rec.Content {
		t.Errorf("content mismatch: got %q", got[0].Content)
	}
}

func TestReadWrite_LargeCount(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	records := make([]parquet.MemoryRecord, 10000)
	for i := range records {
		records[i] = makeRecord(fmt.Sprintf("%d", i), base.Add(time.Duration(i)*time.Second))
	}
	got := writeRead(t, records, time.Time{})
	if len(got) != 10000 {
		t.Fatalf("got %d records want 10000", len(got))
	}
}
