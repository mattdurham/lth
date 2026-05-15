// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package main

import (
	"context"
	"encoding/binary"
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/mattdurham/lth/internal/db"
	"github.com/mattdurham/lth/internal/vector"
)

// testDB opens a fresh DB in a temp dir and registers cleanup.
func testDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

// makeEmb creates a deterministic 768-float32 embedding encoded as little-endian bytes.
func makeEmb(seed float32) []byte {
	const dims = 768
	b := make([]byte, dims*4)
	for i := 0; i < dims; i++ {
		val := seed * float32(i+1) / float32(dims)
		binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(val))
	}
	return b
}

// seedRow builds a MemoryRow with all fields populated.
func seedRow(id string, layer int, seed float32, now time.Time) *db.MemoryRow {
	return &db.MemoryRow{
		ID:             id,
		Layer:          layer,
		Content:        "content for " + id,
		ContentHash:    "hash-" + id,
		Embedding:      makeEmb(seed),
		Importance:     seed,
		AccessCount:    int(seed * 10),
		CreatedAt:      now,
		UpdatedAt:      now,
		LastAccessedAt: now,
		DecayRate:      seed * 0.1,
		Stability:      seed * 0.5,
		Source:         "source-" + id,
		Agent:          "agent-" + id,
		Valence:        seed * 0.3,
		ValenceScored:  true,
	}
}

// populateDB inserts 2 memories per layer (layers 1–5), edges, and attrs into d.
// Returns all inserted rows and edges for later comparison.
func populateDB(t *testing.T, d *db.DB) ([]*db.MemoryRow, []*db.EdgeRow) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	var rows []*db.MemoryRow
	for layer := 1; layer <= 5; layer++ {
		for j := 0; j < 2; j++ {
			seed := float32(layer)*0.1 + float32(j)*0.05
			// Clamp valence to [-1,1]: seed*0.3 max = 5*0.1*0.3+0.05*0.3 ≈ 0.165, safely in range.
			r := seedRow(
				"mem-"+string(rune('a'+layer-1))+string(rune('0'+j)),
				layer, seed, now,
			)
			if err := d.InsertMemory(ctx, r); err != nil {
				t.Fatalf("InsertMemory(%s): %v", r.ID, err)
			}
			attrs := map[string]string{
				"cwd":  "/home/user/project-" + r.ID,
				"tags": "layer" + string(rune('0'+layer)) + ",test",
				"idx":  string(rune('0' + j)),
			}
			if err := d.SetAttributes(ctx, r.ID, attrs); err != nil {
				t.Fatalf("SetAttributes(%s): %v", r.ID, err)
			}
			r.Embedding = makeEmb(seed) // keep a copy for comparison
			rows = append(rows, r)
		}
	}

	// Build edges: use some of the inserted memory IDs.
	// "relates_to" between layer-1 memories, "compacted_from" between layer-5 and layer-4.
	edges := []*db.EdgeRow{
		{
			ID:        "edge-001",
			FromID:    "mem-a0",
			ToID:      "mem-a1",
			EdgeType:  "relates_to",
			Weight:    0.9,
			CreatedAt: now,
		},
		{
			ID:        "edge-002",
			FromID:    "mem-e0",
			ToID:      "mem-d0",
			EdgeType:  "compacted_from",
			Weight:    1.0,
			CreatedAt: now,
		},
		{
			ID:        "edge-003",
			FromID:    "mem-b0",
			ToID:      "mem-c1",
			EdgeType:  "supports",
			Weight:    0.75,
			CreatedAt: now,
		},
	}
	for _, e := range edges {
		if err := d.InsertEdge(ctx, e); err != nil {
			t.Fatalf("InsertEdge(%s): %v", e.ID, err)
		}
	}

	return rows, edges
}

func TestExportImportRoundtrip(t *testing.T) {
	ctx := context.Background()

	srcDB := testDB(t)
	origRows, origEdges := populateDB(t, srcDB)

	zipPath := filepath.Join(t.TempDir(), "export.zip")
	memCount, edgeCount, err := exportDB(ctx, srcDB, zipPath, 1000)
	if err != nil {
		t.Fatalf("exportDB: %v", err)
	}
	if memCount != len(origRows) {
		t.Errorf("exportDB memCount = %d, want %d", memCount, len(origRows))
	}
	if edgeCount != len(origEdges) {
		t.Errorf("exportDB edgeCount = %d, want %d", edgeCount, len(origEdges))
	}

	dstDB := testDB(t)
	impMem, impEdge, skipped, err := importDB(ctx, dstDB, zipPath, false, false)
	if err != nil {
		t.Fatalf("importDB: %v", err)
	}
	if impMem != len(origRows) {
		t.Errorf("importDB memories = %d, want %d", impMem, len(origRows))
	}
	if impEdge != len(origEdges) {
		t.Errorf("importDB edges = %d, want %d", impEdge, len(origEdges))
	}
	if skipped != 0 {
		t.Errorf("importDB skipped = %d, want 0", skipped)
	}

	// Pre-fetch source attrs for comparison.
	origIDs := make([]string, len(origRows))
	for i, r := range origRows {
		origIDs[i] = r.ID
	}
	srcAttrs, err := srcDB.GetAttributesBatch(ctx, origIDs)
	if err != nil {
		t.Fatalf("GetAttributesBatch(src): %v", err)
	}

	// Verify every memory field exactly.
	for _, orig := range origRows {
		got, err := dstDB.GetMemory(ctx, orig.ID)
		if err != nil {
			t.Errorf("GetMemory(%s): %v", orig.ID, err)
			continue
		}
		assertMemoryEqual(t, orig, got)

		// Verify attrs match source.
		gotAttrs, err := dstDB.GetAttributes(ctx, orig.ID)
		if err != nil {
			t.Errorf("GetAttributes(%s): %v", orig.ID, err)
			continue
		}
		wantAttrs := srcAttrs[orig.ID]
		for k, wantV := range wantAttrs {
			if gotAttrs[k] != wantV {
				t.Errorf("memory %s attr[%s] = %q, want %q", orig.ID, k, gotAttrs[k], wantV)
			}
		}
		if len(gotAttrs) != len(wantAttrs) {
			t.Errorf("memory %s attr count = %d, want %d", orig.ID, len(gotAttrs), len(wantAttrs))
		}
	}

	// Verify every edge field exactly.
	allEdges, err := dstDB.GetAllEdges(ctx)
	if err != nil {
		t.Fatalf("GetAllEdges on dst: %v", err)
	}
	if len(allEdges) != len(origEdges) {
		t.Errorf("edge count = %d, want %d", len(allEdges), len(origEdges))
	}
	edgeByID := make(map[string]*db.EdgeRow, len(allEdges))
	for _, e := range allEdges {
		edgeByID[e.ID] = e
	}
	for _, orig := range origEdges {
		got, ok := edgeByID[orig.ID]
		if !ok {
			t.Errorf("edge %s missing from imported DB", orig.ID)
			continue
		}
		if got.FromID != orig.FromID {
			t.Errorf("edge %s FromID = %q, want %q", orig.ID, got.FromID, orig.FromID)
		}
		if got.ToID != orig.ToID {
			t.Errorf("edge %s ToID = %q, want %q", orig.ID, got.ToID, orig.ToID)
		}
		if got.EdgeType != orig.EdgeType {
			t.Errorf("edge %s EdgeType = %q, want %q", orig.ID, got.EdgeType, orig.EdgeType)
		}
		if got.Weight != orig.Weight {
			t.Errorf("edge %s Weight = %v, want %v", orig.ID, got.Weight, orig.Weight)
		}
	}
}

func TestExportImportIdempotent(t *testing.T) {
	ctx := context.Background()

	srcDB := testDB(t)
	origRows, origEdges := populateDB(t, srcDB)

	zipPath := filepath.Join(t.TempDir(), "export.zip")
	if _, _, err := exportDB(ctx, srcDB, zipPath, 1000); err != nil {
		t.Fatalf("exportDB: %v", err)
	}

	dstDB := testDB(t)
	if _, _, _, err := importDB(ctx, dstDB, zipPath, false, false); err != nil {
		t.Fatalf("first importDB: %v", err)
	}

	// Second import with skip-existing — must not error, all records skipped.
	_, _, skipped, err := importDB(ctx, dstDB, zipPath, false, true)
	if err != nil {
		t.Fatalf("second importDB (skip-existing): %v", err)
	}
	// All memories skipped (edges use INSERT OR IGNORE so they count as inserted, not skipped).
	if skipped != len(origRows) {
		t.Errorf("second import skipped = %d, want %d (one per memory)", skipped, len(origRows))
	}

	// DB state unchanged: same memory and edge counts.
	for _, orig := range origRows {
		if _, err := dstDB.GetMemory(ctx, orig.ID); err != nil {
			t.Errorf("GetMemory(%s) after idempotent import: %v", orig.ID, err)
		}
	}
	allEdges, err := dstDB.GetAllEdges(ctx)
	if err != nil {
		t.Fatalf("GetAllEdges: %v", err)
	}
	if len(allEdges) != len(origEdges) {
		t.Errorf("edge count after idempotent import = %d, want %d", len(allEdges), len(origEdges))
	}
}

// assertMemoryEqual checks every field of two MemoryRows for equality.
func assertMemoryEqual(t *testing.T, orig, got *db.MemoryRow) {
	t.Helper()
	if got.ID != orig.ID {
		t.Errorf("ID: got %q want %q", got.ID, orig.ID)
	}
	if got.Layer != orig.Layer {
		t.Errorf("%s Layer: got %d want %d", orig.ID, got.Layer, orig.Layer)
	}
	if got.Content != orig.Content {
		t.Errorf("%s Content: got %q want %q", orig.ID, got.Content, orig.Content)
	}
	if got.ContentHash != orig.ContentHash {
		t.Errorf("%s ContentHash: got %q want %q", orig.ID, got.ContentHash, orig.ContentHash)
	}
	if got.Importance != orig.Importance {
		t.Errorf("%s Importance: got %v want %v", orig.ID, got.Importance, orig.Importance)
	}
	if got.AccessCount != orig.AccessCount {
		t.Errorf("%s AccessCount: got %d want %d", orig.ID, got.AccessCount, orig.AccessCount)
	}
	if got.DecayRate != orig.DecayRate {
		t.Errorf("%s DecayRate: got %v want %v", orig.ID, got.DecayRate, orig.DecayRate)
	}
	if got.Stability != orig.Stability {
		t.Errorf("%s Stability: got %v want %v", orig.ID, got.Stability, orig.Stability)
	}
	if got.Source != orig.Source {
		t.Errorf("%s Source: got %q want %q", orig.ID, got.Source, orig.Source)
	}
	if got.Agent != orig.Agent {
		t.Errorf("%s Agent: got %q want %q", orig.ID, got.Agent, orig.Agent)
	}
	if got.Valence != orig.Valence {
		t.Errorf("%s Valence: got %v want %v", orig.ID, got.Valence, orig.Valence)
	}
	if got.ValenceScored != orig.ValenceScored {
		t.Errorf("%s ValenceScored: got %v want %v", orig.ID, got.ValenceScored, orig.ValenceScored)
	}
	if !got.CreatedAt.Equal(orig.CreatedAt) {
		t.Errorf("%s CreatedAt: got %v want %v", orig.ID, got.CreatedAt, orig.CreatedAt)
	}

	// Compare embeddings element-by-element via FromBytes.
	origFloats := vector.FromBytes(orig.Embedding)
	gotFloats := vector.FromBytes(got.Embedding)
	if len(gotFloats) != len(origFloats) {
		t.Errorf("%s Embedding length: got %d want %d", orig.ID, len(gotFloats), len(origFloats))
	} else {
		for i, f := range origFloats {
			if gotFloats[i] != f {
				t.Errorf("%s Embedding[%d]: got %v want %v", orig.ID, i, gotFloats[i], f)
				break
			}
		}
	}
}
