// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package db

import (
	"context"
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
