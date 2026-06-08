package db

import (
	"context"
	"encoding/binary"
	"math"
	"testing"
)

// TestVec0Roundtrip verifies that bytes inserted into memories_vec come back
// byte-for-byte identical via SELECT embedding FROM memories_vec WHERE rowid = ?.
// If sqlite-vec compresses or quantizes vectors by default, this test fails and
// we cannot use vec0 as the sole embedding store without precision loss.
func TestVec0Roundtrip(t *testing.T) {
	d := openTempDB(t)
	defer d.Close() //nolint:errcheck

	// Build a known embedding with distinctive float values across the full dynamic range.
	const dim = 768
	known := make([]float32, dim)
	for i := 0; i < dim; i++ {
		known[i] = float32(i)*0.0123456789 - 4.2
	}
	knownBytes := make([]byte, dim*4)
	for i, v := range known {
		binary.LittleEndian.PutUint32(knownBytes[i*4:], math.Float32bits(v))
	}

	row := &MemoryRow{
		ID: "roundtrip-1", Layer: 5, Content: "x", ContentHash: "rt-h",
		Embedding: knownBytes, Importance: 5.0,
	}
	if err := d.InsertMemory(context.Background(), row); err != nil {
		t.Fatalf("insert: %v", err)
	}

	// Look up the actual rowid via content_hash.
	var rowid int64
	if err := d.db.QueryRowContext(context.Background(),
		`SELECT rowid FROM memories WHERE content_hash = ?`, "rt-h",
	).Scan(&rowid); err != nil {
		t.Fatalf("get rowid: %v", err)
	}

	// Read embedding back from vec0.
	var got []byte
	if err := d.db.QueryRowContext(context.Background(),
		`SELECT embedding FROM memories_vec WHERE rowid = ?`, rowid,
	).Scan(&got); err != nil {
		t.Fatalf("read vec0 embedding: %v", err)
	}
	t.Logf("inserted %d bytes; read back %d bytes from vec0", len(knownBytes), len(got))
	if len(got) != len(knownBytes) {
		t.Fatalf("length mismatch: got %d want %d (vec0 may compress)", len(got), len(knownBytes))
	}
	for i := range got {
		if got[i] != knownBytes[i] {
			t.Fatalf("byte %d differs: got 0x%02x want 0x%02x (vec0 not byte-identical)", i, got[i], knownBytes[i])
		}
	}

	// Bonus: batch query (WHERE rowid IN (...)) — verify this also works.
	rows, err := d.db.QueryContext(context.Background(),
		`SELECT rowid, embedding FROM memories_vec WHERE rowid IN (?, ?)`, rowid, rowid+9999)
	if err != nil {
		t.Fatalf("batch query: %v", err)
	}
	defer rows.Close() //nolint:errcheck
	var n int
	for rows.Next() {
		var r int64
		var b []byte
		if err := rows.Scan(&r, &b); err != nil {
			t.Fatalf("scan batch: %v", err)
		}
		n++
		t.Logf("batch row rowid=%d bytes=%d", r, len(b))
	}
	if n != 1 {
		t.Errorf("expected 1 batch row, got %d", n)
	}
}

func TestVec0JoinByID(t *testing.T) {
	d := openTempDB(t)
	defer d.Close() //nolint:errcheck

	const dim = 768
	knownBytes := make([]byte, dim*4)
	for i := range knownBytes {
		knownBytes[i] = byte(i * 7 & 0xff)
	}
	for i, id := range []string{"join-a", "join-b", "join-c"} {
		_ = i
		row := &MemoryRow{
			ID: id, Layer: 5, Content: "x", ContentHash: "h-" + id,
			Embedding: knownBytes, Importance: 5.0,
		}
		if err := d.InsertMemory(context.Background(), row); err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}

	// Try the join-by-id query — does vec0 allow this?
	rows, err := d.db.QueryContext(context.Background(),
		`SELECT m.id, mv.embedding FROM memories_vec mv
		 JOIN memories m ON m.rowid = mv.rowid
		 WHERE m.id IN (?, ?, ?)`,
		"join-a", "join-b", "join-c")
	if err != nil {
		t.Fatalf("join query: %v", err)
	}
	defer rows.Close() //nolint:errcheck
	got := map[string]int{}
	for rows.Next() {
		var id string
		var b []byte
		if err := rows.Scan(&id, &b); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got[id] = len(b)
	}
	t.Logf("join-by-id returned: %v", got)
	if len(got) != 3 {
		t.Errorf("expected 3 rows, got %d", len(got))
	}
}

func TestScanFallbackFromVec0(t *testing.T) {
	// Regression: after the dual-store-removal migration, memories.embedding is
	// always NULL for vec0-present rows. scanMemoryRow(s) must transparently fill
	// m.Embedding from vec0 so callers (compactor, search) keep working.
	d := openTempDB(t)
	defer d.Close() //nolint:errcheck

	const dim = 768
	known := make([]byte, dim*4)
	for i := 0; i < dim; i++ {
		binary.LittleEndian.PutUint32(known[i*4:], math.Float32bits(float32(i)*0.01))
	}

	// Insert with embedding (writes to vec0, leaves BLOB NULL per new behaviour).
	row := &MemoryRow{
		ID: "fb-1", Layer: 5, Content: "c", ContentHash: "fb-h",
		Embedding: known, Importance: 5,
	}
	if err := d.InsertMemory(context.Background(), row); err != nil {
		t.Fatalf("insert: %v", err)
	}

	// Verify BLOB is actually NULL in storage.
	var blob []byte
	if err := d.db.QueryRowContext(context.Background(),
		`SELECT embedding FROM memories WHERE id = ?`, "fb-1").Scan(&blob); err != nil {
		t.Fatalf("raw select: %v", err)
	}
	if len(blob) != 0 {
		t.Fatalf("expected BLOB NULL after InsertMemory, got %d bytes", len(blob))
	}

	// Now scan via GetMemory — should populate m.Embedding via vec0 fallback.
	got, err := d.GetMemory(context.Background(), "fb-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got.Embedding) != len(known) {
		t.Fatalf("scan fallback len mismatch: got %d want %d", len(got.Embedding), len(known))
	}
	for i := range known {
		if got.Embedding[i] != known[i] {
			t.Fatalf("byte %d differs after scan fallback", i)
		}
	}

	// Batch path: insert several rows, list, check all have embedding populated.
	for i := 0; i < 5; i++ {
		r := &MemoryRow{
			ID: "fb-batch-" + intToStr(i), Layer: 5, Content: "c", ContentHash: "fb-bh-" + intToStr(i),
			Embedding: known, Importance: 5,
		}
		if err := d.InsertMemory(context.Background(), r); err != nil {
			t.Fatalf("insert batch %d: %v", i, err)
		}
	}
	all, err := d.ListLayer(context.Background(), 5, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 6 {
		t.Fatalf("expected 6 L5 rows, got %d", len(all))
	}
	for i, m := range all {
		if len(m.Embedding) != len(known) {
			t.Errorf("row[%d] %s: embedding len=%d, want %d", i, m.ID, len(m.Embedding), len(known))
		}
	}
}
