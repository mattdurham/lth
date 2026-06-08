// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package db

import (
	"context"
	"encoding/binary"
	"math"
	"testing"
)

func TestMigrateOrphanEmbeddings(t *testing.T) {
	d := openTempDB(t)
	defer d.Close() //nolint:errcheck
	ctx := context.Background()

	// Build a valid 768-dim embedding.
	const dim = 768
	emb := make([]byte, dim*4)
	for i := 0; i < dim; i++ {
		binary.LittleEndian.PutUint32(emb[i*4:], math.Float32bits(float32(i)*0.001))
	}

	// Simulate the legacy bug: insert rows with no embedding via InsertMemory
	// (so they go through the new code path, no vec0 entry), then directly
	// UPDATE the BLOB column (bypassing UpdateEmbedding), leaving them
	// orphaned: BLOB has data, vec0 does not.
	const n = 12
	// Use full-length 36-char UUIDs so GetMemory does an exact match (it falls
	// back to prefix matching for shorter ids, which would collide with
	// substring-shared ids like "orphan-1" vs "orphan-10").
	makeID := func(i int) string {
		base := "00000000-0000-0000-0000-orphan" // 30 chars
		suffix := intToStr(i)
		for len(suffix) < 6 {
			suffix = "0" + suffix
		}
		return base + suffix // total 36 chars
	}
	for i := 0; i < n; i++ {
		row := &MemoryRow{
			ID: makeID(i), Layer: 5, Content: "c", ContentHash: "h-" + intToStr(i),
			Importance: 5,
		}
		if err := d.InsertMemory(ctx, row); err != nil {
			t.Fatalf("insert %d: %v", i, err)
		}
		// Directly stuff the BLOB without going through vec0 \u2014 simulates the bug.
		if _, err := d.db.ExecContext(ctx,
			`UPDATE memories SET embedding = ? WHERE id = ?`, emb, row.ID); err != nil {
			t.Fatalf("stuff blob %d: %v", i, err)
		}
	}

	before, err := d.CountOrphanEmbeddings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if before != n {
		t.Fatalf("CountOrphanEmbeddings before = %d, want %d", before, n)
	}

	if err := d.MigrateOrphanEmbeddings(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	after, err := d.CountOrphanEmbeddings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if after != 0 {
		t.Errorf("orphans remaining after migration: %d", after)
	}

	// Verify each orphan can now be read via scan with its embedding populated
	// from vec0 (and the BLOB is NULL).
	for i := 0; i < n; i++ {
		id := makeID(i)
		m, err := d.GetMemory(ctx, id)
		if err != nil {
			t.Fatalf("get %s: %v", id, err)
		}
		if len(m.Embedding) != len(emb) {
			t.Errorf("%s: embedding len=%d want %d", id, len(m.Embedding), len(emb))
		}
		// Confirm BLOB is NULL.
		var blob []byte
		if err := d.db.QueryRowContext(ctx,
			`SELECT embedding FROM memories WHERE id = ?`, id).Scan(&blob); err != nil {
			t.Fatal(err)
		}
		if len(blob) != 0 {
			t.Errorf("%s: BLOB still has %d bytes after migration", id, len(blob))
		}
	}

	// Second call must be a no-op.
	if err := d.MigrateOrphanEmbeddings(ctx); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
}
