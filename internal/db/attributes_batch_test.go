// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package db

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestGetAttributesBatch_Empty(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()

	result, err := d.GetAttributesBatch(ctx, []string{})
	if err != nil {
		t.Fatalf("GetAttributesBatch(empty): %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty map, got %d entries", len(result))
	}
}

func TestGetAttributesBatch_SingleID(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()

	now := time.Now().UTC()
	row := &MemoryRow{
		ID:          "attr-batch-001",
		Layer:       3,
		Content:     "attrs batch test",
		ContentHash: "abatch001",
		Importance:  5.0,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := d.InsertMemory(ctx, row); err != nil {
		t.Fatalf("InsertMemory: %v", err)
	}
	if err := d.SetAttributes(ctx, "attr-batch-001", map[string]string{"key1": "val1", "key2": "val2"}); err != nil {
		t.Fatalf("SetAttributes: %v", err)
	}

	result, err := d.GetAttributesBatch(ctx, []string{"attr-batch-001"})
	if err != nil {
		t.Fatalf("GetAttributesBatch: %v", err)
	}

	attrs, ok := result["attr-batch-001"]
	if !ok {
		t.Fatal("expected entry for attr-batch-001")
	}
	if attrs["key1"] != "val1" {
		t.Errorf("key1 = %q, want %q", attrs["key1"], "val1")
	}
	if attrs["key2"] != "val2" {
		t.Errorf("key2 = %q, want %q", attrs["key2"], "val2")
	}
}

func TestGetAttributesBatch_MultipleIDs(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()

	now := time.Now().UTC()
	for _, id := range []string{"batch-a", "batch-b"} {
		row := &MemoryRow{
			ID:          id,
			Layer:       3,
			Content:     "content " + id,
			ContentHash: "hash-" + id,
			Importance:  5.0,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		if err := d.InsertMemory(ctx, row); err != nil {
			t.Fatalf("InsertMemory(%s): %v", id, err)
		}
	}
	if err := d.SetAttributes(ctx, "batch-a", map[string]string{"a": "1"}); err != nil {
		t.Fatalf("SetAttributes(batch-a): %v", err)
	}
	if err := d.SetAttributes(ctx, "batch-b", map[string]string{"b": "2"}); err != nil {
		t.Fatalf("SetAttributes(batch-b): %v", err)
	}

	result, err := d.GetAttributesBatch(ctx, []string{"batch-a", "batch-b"})
	if err != nil {
		t.Fatalf("GetAttributesBatch: %v", err)
	}

	if result["batch-a"]["a"] != "1" {
		t.Errorf("batch-a.a = %q, want 1", result["batch-a"]["a"])
	}
	if result["batch-b"]["b"] != "2" {
		t.Errorf("batch-b.b = %q, want 2", result["batch-b"]["b"])
	}
	// Ensure no cross-contamination.
	if _, ok := result["batch-a"]["b"]; ok {
		t.Error("batch-a should not have key b")
	}
}

func TestGetAttributesBatch_MissingID(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()

	now := time.Now().UTC()
	row := &MemoryRow{
		ID:          "batch-real",
		Layer:       3,
		Content:     "real memory",
		ContentHash: "batchrealhash",
		Importance:  5.0,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := d.InsertMemory(ctx, row); err != nil {
		t.Fatalf("InsertMemory: %v", err)
	}
	if err := d.SetAttributes(ctx, "batch-real", map[string]string{"x": "y"}); err != nil {
		t.Fatalf("SetAttributes: %v", err)
	}

	result, err := d.GetAttributesBatch(ctx, []string{"batch-real", "does-not-exist"})
	if err != nil {
		t.Fatalf("GetAttributesBatch: %v", err)
	}

	// Real ID should have its attrs.
	if result["batch-real"]["x"] != "y" {
		t.Errorf("batch-real.x = %q, want y", result["batch-real"]["x"])
	}

	// Missing ID should have empty map (not nil, not absent).
	missing, ok := result["does-not-exist"]
	if !ok {
		t.Fatal("expected entry for does-not-exist (empty map)")
	}
	if len(missing) != 0 {
		t.Errorf("does-not-exist attrs should be empty, got %v", missing)
	}
}

// TestGetAttributesBatch_ExceedsMaxIDsChunksCorrectly regression-tests a real
// production incident: exportDBFiltered (used by both `lth sync push` and
// `lth export`) calls GetAttributesBatch with every ID in a layer in one
// slice. Once a layer grew past SQLite's bound-parameter limit, the
// unchunked "IN (...)" query failed every single call with "too many SQL
// variables" -- silently breaking sync/export entirely for large layers.
// This inserts more rows than attributesBatchMaxIDs to prove the chunking
// loop still returns correct, complete results (not just "doesn't error").
func TestGetAttributesBatch_ExceedsMaxIDsChunksCorrectly(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	now := time.Now().UTC()

	n := attributesBatchMaxIDs + 250 // spans three chunks at the boundary
	ids := make([]string, n)
	for i := range n {
		id := fmt.Sprintf("big-batch-%d", i)
		ids[i] = id
		row := &MemoryRow{
			ID:          id,
			Layer:       3,
			Content:     "content " + id,
			ContentHash: "hash-" + id,
			Importance:  5.0,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		if err := d.InsertMemory(ctx, row); err != nil {
			t.Fatalf("InsertMemory(%s): %v", id, err)
		}
		if err := d.SetAttributes(ctx, id, map[string]string{"idx": fmt.Sprintf("%d", i)}); err != nil {
			t.Fatalf("SetAttributes(%s): %v", id, err)
		}
	}

	result, err := d.GetAttributesBatch(ctx, ids)
	if err != nil {
		t.Fatalf("GetAttributesBatch with %d ids (> attributesBatchMaxIDs=%d): %v", n, attributesBatchMaxIDs, err)
	}
	if len(result) != n {
		t.Fatalf("result has %d entries, want %d", len(result), n)
	}
	for i, id := range ids {
		want := fmt.Sprintf("%d", i)
		if got := result[id]["idx"]; got != want {
			t.Errorf("result[%s][idx] = %q, want %q", id, got, want)
		}
	}
}

func TestGetMemIDsByAttr(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	now := time.Now().UTC()

	for _, id := range []string{"mem-ta", "mem-tb", "mem-tc"} {
		row := &MemoryRow{
			ID:          id,
			Layer:       3,
			Content:     "content " + id,
			ContentHash: "hash-" + id,
			Importance:  5.0,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		if err := d.InsertMemory(ctx, row); err != nil {
			t.Fatalf("InsertMemory(%s): %v", id, err)
		}
	}

	// mem-ta and mem-tb have trace_id=abc123; mem-tc has a different trace_id.
	if err := d.SetAttributes(ctx, "mem-ta", map[string]string{"trace_id": "abc123"}); err != nil {
		t.Fatalf("SetAttributes(mem-ta): %v", err)
	}
	if err := d.SetAttributes(ctx, "mem-tb", map[string]string{"trace_id": "abc123"}); err != nil {
		t.Fatalf("SetAttributes(mem-tb): %v", err)
	}
	if err := d.SetAttributes(ctx, "mem-tc", map[string]string{"trace_id": "xyz999"}); err != nil {
		t.Fatalf("SetAttributes(mem-tc): %v", err)
	}

	ids, err := d.GetMemIDsByAttr(ctx, "trace_id", "abc123")
	if err != nil {
		t.Fatalf("GetMemIDsByAttr: %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("GetMemIDsByAttr returned %d ids, want 2: %v", len(ids), ids)
	}
	got := map[string]bool{}
	for _, id := range ids {
		got[id] = true
	}
	if !got["mem-ta"] {
		t.Error("expected mem-ta in results")
	}
	if !got["mem-tb"] {
		t.Error("expected mem-tb in results")
	}
}
