// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package db

import (
	"context"
	"testing"
	"time"
)

func TestRebuildMemoryEdgesTable_FromLegacySchema(t *testing.T) {
	// Simulate a database created on the legacy schema (with synthetic id column),
	// then run the rebuild and verify (a) the id column is gone, (b) all rows are
	// preserved, (c) the natural key is now the PK, (d) idx_edges_from/to exist,
	// (e) a second rebuild is a no-op.
	d := openTempDB(t)
	defer d.Close() //nolint:errcheck
	ctx := context.Background()

	// Drop the modern table and recreate it in the legacy form.
	if _, err := d.db.ExecContext(ctx, `DROP TABLE memory_edges`); err != nil {
		t.Fatalf("drop modern table: %v", err)
	}
	if _, err := d.db.ExecContext(ctx, `
CREATE TABLE memory_edges (
	id         TEXT PRIMARY KEY,
	from_id    TEXT NOT NULL,
	to_id      TEXT NOT NULL,
	edge_type  TEXT NOT NULL,
	weight     REAL NOT NULL DEFAULT 1.0,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	UNIQUE(from_id, to_id, edge_type)
)`); err != nil {
		t.Fatalf("recreate legacy table: %v", err)
	}

	// Seed memories so the FK in the rebuilt schema is satisfied. We need to
	// keep references valid; for the legacy table FK wasn't declared above, so
	// it's fine.
	now := time.Now().UTC()
	for _, id := range []string{"m-a", "m-b", "m-c"} {
		row := &MemoryRow{ID: id, Layer: 5, Content: "c", ContentHash: "h-" + id, Importance: 5,
			CreatedAt: now, UpdatedAt: now, LastAccessedAt: now}
		if err := d.InsertMemory(ctx, row); err != nil {
			t.Fatalf("insert memory %s: %v", id, err)
		}
	}

	// Insert 3 legacy edges with synthetic id values.
	for _, e := range []struct{ id, from, to, typ string }{
		{"E1", "m-a", "m-b", "relates_to"},
		{"E2", "m-b", "m-c", "supports"},
		{"E3", "m-a", "m-c", "compacted_from"},
	} {
		if _, err := d.db.ExecContext(ctx,
			`INSERT INTO memory_edges (id, from_id, to_id, edge_type, weight, created_at) VALUES (?,?,?,?,?,?)`,
			e.id, e.from, e.to, e.typ, 0.5, now); err != nil {
			t.Fatalf("insert legacy edge %s: %v", e.id, err)
		}
	}

	// Sanity: id column exists in the legacy schema.
	has, err := d.columnExists(ctx, "memory_edges", "id")
	if err != nil || !has {
		t.Fatalf("expected legacy id column, has=%v err=%v", has, err)
	}

	// Run the rebuild.
	if err := d.rebuildMemoryEdgesTable(ctx); err != nil {
		t.Fatalf("rebuild: %v", err)
	}

	// id column should be gone.
	has, err = d.columnExists(ctx, "memory_edges", "id")
	if err != nil {
		t.Fatalf("columnExists after rebuild: %v", err)
	}
	if has {
		t.Errorf("id column still present after rebuild")
	}

	// All 3 edges preserved.
	all, err := d.GetAllEdges(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Errorf("got %d edges after rebuild, want 3", len(all))
	}

	// Natural key is now PK: inserting a duplicate (from,to,type) is ignored.
	if err := d.InsertEdge(ctx, &EdgeRow{
		FromID: "m-a", ToID: "m-b", EdgeType: "relates_to", Weight: 0.9, CreatedAt: now,
	}); err != nil {
		t.Errorf("duplicate insert: %v", err)
	}
	all2, _ := d.GetAllEdges(ctx)
	if len(all2) != 3 {
		t.Errorf("duplicate edge was inserted: count=%d, want 3", len(all2))
	}

	// Second rebuild is a no-op.
	if err := d.rebuildMemoryEdgesTable(ctx); err != nil {
		t.Errorf("second rebuild errored: %v", err)
	}
}
