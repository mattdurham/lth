// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package memory

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/mattdurham/lth/internal/db"
	"github.com/mattdurham/lth/internal/vector"
)

// selectiveEmbedder fails with a configurable error for one specific memory ID
// (identified by content) and succeeds for everything else.
type selectiveEmbedder struct {
	failContent string
	failErr     error
}

func (s *selectiveEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	if text == s.failContent {
		return nil, s.failErr
	}
	return []float32{0.1, 0.2, 0.3}, nil
}

func (s *selectiveEmbedder) Dims() int { return 3 }

func backfillTestDB(t *testing.T) *db.DB {
	t.Helper()
	dir := t.TempDir()
	d, err := db.Open(filepath.Join(dir, "test.db"), 3)
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

func insertUnembeddedRow(t *testing.T, d *db.DB, id, content string) {
	t.Helper()
	now := time.Now().UTC()
	row := &db.MemoryRow{
		ID:             id,
		Layer:          5,
		Content:        content,
		ContentHash:    id, // unique per test row, doesn't need to be a real hash
		Importance:     5.0,
		CreatedAt:      now,
		UpdatedAt:      now,
		LastAccessedAt: now,
		Stability:      1.0,
	}
	if err := d.InsertMemory(context.Background(), row); err != nil {
		t.Fatalf("InsertMemory: %v", err)
	}
}

func TestBackfillEmbeddings_GivesUpOnPayloadTooLarge(t *testing.T) {
	d := backfillTestDB(t)
	insertUnembeddedRow(t, d, "too-large-id", "huge content")
	insertUnembeddedRow(t, d, "normal-id", "normal content")

	emb := &selectiveEmbedder{failContent: "huge content", failErr: vector.ErrPayloadTooLarge}

	giveUps := 0
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		BackfillEmbeddings(ctx, d, emb, "test-model", 10, 10*time.Millisecond, func() { giveUps++ })
	}()

	// One batch is enough to process both rows; give it a moment then stop.
	time.Sleep(50 * time.Millisecond)
	cancel()
	time.Sleep(20 * time.Millisecond)

	if giveUps != 1 {
		t.Errorf("onGiveUp called %d times, want 1", giveUps)
	}

	tooLarge, err := d.GetMemory(context.Background(), "too-large-id")
	if err != nil {
		t.Fatalf("GetMemory(too-large-id): %v", err)
	}
	if tooLarge.CompactedAt == nil {
		t.Error("too-large-id should be soft-deleted (CompactedAt set) after a permanent embed failure")
	}

	normal, err := d.GetMemory(context.Background(), "normal-id")
	if err != nil {
		t.Fatalf("GetMemory(normal-id): %v", err)
	}
	if normal.CompactedAt != nil {
		t.Error("normal-id should NOT be soft-deleted -- it embedded successfully")
	}
	if len(normal.Embedding) == 0 && normal.EmbeddingModel == "" {
		t.Error("normal-id should have received an embedding")
	}
}

func TestBackfillEmbeddings_TransientErrorDoesNotSoftDelete(t *testing.T) {
	d := backfillTestDB(t)
	insertUnembeddedRow(t, d, "flaky-id", "flaky content")

	emb := &selectiveEmbedder{failContent: "flaky content", failErr: errors.New("connection refused")}

	giveUps := 0
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		BackfillEmbeddings(ctx, d, emb, "test-model", 10, 10*time.Millisecond, func() { giveUps++ })
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()
	time.Sleep(20 * time.Millisecond)

	if giveUps != 0 {
		t.Errorf("onGiveUp called %d times, want 0 -- a transient error must not be treated as permanent", giveUps)
	}

	row, err := d.GetMemory(context.Background(), "flaky-id")
	if err != nil {
		t.Fatalf("GetMemory(flaky-id): %v", err)
	}
	if row.CompactedAt != nil {
		t.Error("flaky-id should NOT be soft-deleted -- it should keep being retried every batch")
	}
}
