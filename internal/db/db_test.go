// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package db

import (
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"io/fs"
	"math"
	"path/filepath"
	"testing"
	"time"
)

const testEmbeddingDims = 1024

// makeTestEmbedding creates a deterministic 768-float32 embedding as little-endian bytes.
// The seed controls the direction of the vector.
func makeTestEmbedding(seed float32) []byte {
	b := make([]byte, testEmbeddingDims*4)
	for i := 0; i < testEmbeddingDims; i++ {
		val := seed * float32(i+1) / float32(testEmbeddingDims)
		binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(val))
	}
	return b
}

// testDB opens a fresh in-memory (temp dir) database and registers cleanup.
func testDB(t *testing.T) *DB {
	t.Helper()
	dir := t.TempDir()
	d, err := Open(filepath.Join(dir, "test.db"), 1024)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

func TestOpen(t *testing.T) {
	d := testDB(t)

	ctx := context.Background()

	// Verify WAL mode.
	var journalMode string
	row := d.db.QueryRowContext(ctx, "PRAGMA journal_mode")
	if err := row.Scan(&journalMode); err != nil {
		t.Fatalf("PRAGMA journal_mode: %v", err)
	}
	if journalMode != "wal" {
		t.Errorf("journal_mode = %q, want wal", journalMode)
	}

	// Verify foreign keys.
	var fkEnabled int
	row = d.db.QueryRowContext(ctx, "PRAGMA foreign_keys")
	if err := row.Scan(&fkEnabled); err != nil {
		t.Fatalf("PRAGMA foreign_keys: %v", err)
	}
	if fkEnabled != 1 {
		t.Errorf("foreign_keys = %d, want 1", fkEnabled)
	}

	// Verify all expected tables exist.
	expected := []string{
		"memories", "memories_vec", "memories_fts",
		"memory_attributes", "memory_edges",
		"compaction_log", "db_metadata",
	}
	for _, tbl := range expected {
		var name string
		err := d.db.QueryRowContext(ctx,
			"SELECT name FROM sqlite_master WHERE type IN ('table','virtual_table','shadow') AND name = ?", tbl).Scan(&name)
		if err != nil {
			// Try without type filter for virtual tables
			err2 := d.db.QueryRowContext(ctx,
				"SELECT name FROM sqlite_master WHERE name = ?", tbl).Scan(&name)
			if err2 != nil {
				t.Errorf("table %q not found in sqlite_master", tbl)
			}
		}
	}
}

func TestOpenTwice(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")

	d1, err := Open(path, 0)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	if err := d1.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	d2, err := Open(path, 0)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	defer d2.Close() //nolint:errcheck
}

func TestWithTx(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()

	// Commit path: insert into db_metadata and verify it persists.
	// Rollback path: fn returns error; changes must not persist.
	rollbackErr := d.WithTx(ctx, func(tx *sql.Tx) error {
		return errors.New("intentional rollback")
	})
	if rollbackErr == nil {
		t.Error("WithTx: expected rollback error, got nil")
	}

	// Commit path: insert into db_metadata and verify it persists.
	commitErr := d.WithTx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "INSERT OR IGNORE INTO db_metadata(key,value) VALUES ('txtest','1')")
		return err
	})
	if commitErr != nil {
		t.Fatalf("WithTx commit: %v", commitErr)
	}
	var val string
	if err := d.db.QueryRowContext(ctx, "SELECT value FROM db_metadata WHERE key='txtest'").Scan(&val); err != nil {
		t.Errorf("committed value not found: %v", err)
	}
}

func TestInsertGetMemory(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	row := &MemoryRow{
		ID:             "test-uuid-001",
		Layer:          5,
		Content:        "hello world",
		ContentHash:    "abc123",
		Embedding:      makeTestEmbedding(0.1),
		Importance:     5.0,
		AccessCount:    0,
		CreatedAt:      now,
		UpdatedAt:      now,
		LastAccessedAt: now,
		DecayRate:      0.5,
		Stability:      1.0,
		Source:         "test",
		Agent:          "agent-1",
	}

	if err := d.InsertMemory(ctx, row); err != nil {
		t.Fatalf("InsertMemory: %v", err)
	}

	got, err := d.GetMemory(ctx, "test-uuid-001")
	if err != nil {
		t.Fatalf("GetMemory: %v", err)
	}
	if got.ID != row.ID {
		t.Errorf("ID = %q, want %q", got.ID, row.ID)
	}
	if got.Content != row.Content {
		t.Errorf("Content = %q, want %q", got.Content, row.Content)
	}
	if got.Layer != row.Layer {
		t.Errorf("Layer = %d, want %d", got.Layer, row.Layer)
	}

	// GetByHash
	got2, err := d.GetByHash(ctx, "abc123")
	if err != nil {
		t.Fatalf("GetByHash: %v", err)
	}
	if got2.ID != row.ID {
		t.Errorf("GetByHash: ID = %q, want %q", got2.ID, row.ID)
	}

	// Not found returns fs.ErrNotExist
	_, err = d.GetMemory(ctx, "nonexistent")
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("GetMemory(nonexistent) = %v, want fs.ErrNotExist", err)
	}
}

func TestMarkAccessed(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()

	now := time.Now().UTC()
	row := &MemoryRow{
		ID:             "test-mark-001",
		Layer:          5,
		Content:        "mark accessed test",
		ContentHash:    "markhash",
		Importance:     5.0,
		CreatedAt:      now,
		UpdatedAt:      now,
		LastAccessedAt: now,
		DecayRate:      0.5,
		Stability:      1.0,
	}
	if err := d.InsertMemory(ctx, row); err != nil {
		t.Fatalf("InsertMemory: %v", err)
	}

	for i := 0; i < 2; i++ {
		if err := d.MarkAccessed(ctx, "test-mark-001", time.Now().UTC()); err != nil {
			t.Fatalf("MarkAccessed[%d]: %v", i, err)
		}
	}

	got, err := d.GetMemory(ctx, "test-mark-001")
	if err != nil {
		t.Fatalf("GetMemory: %v", err)
	}
	if got.AccessCount != 2 {
		t.Errorf("AccessCount = %d, want 2", got.AccessCount)
	}
}

func TestSoftDelete(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()

	now := time.Now().UTC()
	row := &MemoryRow{
		ID:          "test-soft-001",
		Layer:       5,
		Content:     "to be soft deleted",
		ContentHash: "softhash",
		Importance:  5.0,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := d.InsertMemory(ctx, row); err != nil {
		t.Fatalf("InsertMemory: %v", err)
	}

	if err := d.SoftDelete(ctx, "test-soft-001", time.Now().UTC()); err != nil {
		t.Fatalf("SoftDelete: %v", err)
	}

	// Should be excluded from active listing
	rows, err := d.ListLayer(ctx, 5, true)
	if err != nil {
		t.Fatalf("ListLayer: %v", err)
	}
	for _, r := range rows {
		if r.ID == "test-soft-001" {
			t.Error("soft-deleted memory found in active ListLayer results")
		}
	}

	// But row still exists in DB
	got, err := d.GetMemory(ctx, "test-soft-001")
	if err != nil {
		t.Fatalf("GetMemory after SoftDelete: %v", err)
	}
	if got.CompactedAt == nil {
		t.Error("CompactedAt should be set after SoftDelete")
	}
}

func TestListLayer(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()

	now := time.Now().UTC()
	for i, layer := range []int{3, 3, 5} {
		row := &MemoryRow{
			ID:          "list-test-" + string(rune('0'+i)),
			Layer:       layer,
			Content:     "content",
			ContentHash: "hash" + string(rune('0'+i)),
			Importance:  5.0,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		if err := d.InsertMemory(ctx, row); err != nil {
			t.Fatalf("InsertMemory[%d]: %v", i, err)
		}
	}

	rows, err := d.ListLayer(ctx, 3, false)
	if err != nil {
		t.Fatalf("ListLayer: %v", err)
	}
	if len(rows) != 2 {
		t.Errorf("ListLayer(3) = %d rows, want 2", len(rows))
	}
}

func TestCountByLayer(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()

	now := time.Now().UTC()
	for i := 0; i < 3; i++ {
		row := &MemoryRow{
			ID:          "count-" + string(rune('0'+i)),
			Layer:       5,
			Content:     "count test",
			ContentHash: "counthash" + string(rune('0'+i)),
			Importance:  5.0,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		if err := d.InsertMemory(ctx, row); err != nil {
			t.Fatalf("InsertMemory[%d]: %v", i, err)
		}
	}

	count, err := d.CountByLayer(ctx, 5)
	if err != nil {
		t.Fatalf("CountByLayer: %v", err)
	}
	if count != 3 {
		t.Errorf("CountByLayer(5) = %d, want 3", count)
	}
}

func TestFTSSearch(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()

	now := time.Now().UTC()
	row := &MemoryRow{
		ID:          "fts-test-001",
		Layer:       3,
		Content:     "golang programming language",
		ContentHash: "ftshash1",
		Importance:  5.0,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := d.InsertMemory(ctx, row); err != nil {
		t.Fatalf("InsertMemory: %v", err)
	}

	results, err := d.FTSSearch(ctx, "golang", []int{3}, 10)
	if err != nil {
		t.Fatalf("FTSSearch: %v", err)
	}

	found := false
	for _, r := range results {
		if r.ID == "fts-test-001" {
			found = true
			break
		}
	}
	if !found {
		t.Error("FTSSearch did not return the expected memory")
	}
}

func TestEdgeCRUD(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()

	now := time.Now().UTC()
	for _, id := range []string{"edge-a", "edge-b"} {
		row := &MemoryRow{
			ID:          id,
			Layer:       3,
			Content:     "edge test " + id,
			ContentHash: id + "-hash",
			Importance:  5.0,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		if err := d.InsertMemory(ctx, row); err != nil {
			t.Fatalf("InsertMemory(%s): %v", id, err)
		}
	}

	edge := &EdgeRow{
		FromID:    "edge-a",
		ToID:      "edge-b",
		EdgeType:  "relates_to",
		Weight:    0.9,
		CreatedAt: now,
	}
	if err := d.InsertEdge(ctx, edge); err != nil {
		t.Fatalf("InsertEdge: %v", err)
	}

	edges, err := d.GetEdges(ctx, "edge-a")
	if err != nil {
		t.Fatalf("GetEdges: %v", err)
	}
	if len(edges) != 1 {
		t.Fatalf("GetEdges = %d edges, want 1", len(edges))
	}
	if edges[0].ToID != "edge-b" {
		t.Errorf("edge ToID = %q, want edge-b", edges[0].ToID)
	}

	neighbors, err := d.GetNeighbors(ctx, "edge-a", []string{"relates_to"})
	if err != nil {
		t.Fatalf("GetNeighbors: %v", err)
	}
	if len(neighbors) != 1 || neighbors[0] != "edge-b" {
		t.Errorf("GetNeighbors = %v, want [edge-b]", neighbors)
	}
}

func TestSetGetAttributes(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()

	now := time.Now().UTC()
	row := &MemoryRow{
		ID:          "attr-test-001",
		Layer:       3,
		Content:     "attribute test",
		ContentHash: "attrhash",
		Importance:  5.0,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := d.InsertMemory(ctx, row); err != nil {
		t.Fatalf("InsertMemory: %v", err)
	}

	attrs := map[string]string{
		"source":  "test",
		"session": "abc123",
	}
	if err := d.SetAttributes(ctx, "attr-test-001", attrs); err != nil {
		t.Fatalf("SetAttributes: %v", err)
	}

	got, err := d.GetAttributes(ctx, "attr-test-001")
	if err != nil {
		t.Fatalf("GetAttributes: %v", err)
	}
	for k, v := range attrs {
		if got[k] != v {
			t.Errorf("attr[%q] = %q, want %q", k, got[k], v)
		}
	}
}

func TestStats(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()

	now := time.Now().UTC()
	for i, layer := range []int{3, 5, 5} {
		row := &MemoryRow{
			ID:          "stats-" + string(rune('0'+i)),
			Layer:       layer,
			Content:     "stats test",
			ContentHash: "statshash" + string(rune('0'+i)),
			Importance:  5.0,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		if err := d.InsertMemory(ctx, row); err != nil {
			t.Fatalf("InsertMemory[%d]: %v", i, err)
		}
	}

	stats, err := d.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.TotalMemories != 3 {
		t.Errorf("TotalMemories = %d, want 3", stats.TotalMemories)
	}
	if stats.ByLayer[3] != 1 {
		t.Errorf("ByLayer[3] = %d, want 1", stats.ByLayer[3])
	}
	if stats.ByLayer[5] != 2 {
		t.Errorf("ByLayer[5] = %d, want 2", stats.ByLayer[5])
	}
}
