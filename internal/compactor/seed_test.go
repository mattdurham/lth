// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package compactor

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/mattdurham/lth/internal/config"
	"github.com/mattdurham/lth/internal/db"
	"github.com/mattdurham/lth/internal/graph"
	"github.com/mattdurham/lth/internal/memory"
)

func seedTestSetup(t *testing.T, llmResp string, llmErr error) (*Compactor, *memory.MemoryStore) {
	t.Helper()
	dir := t.TempDir()
	d, err := db.Open(filepath.Join(dir, "seed_test.db"), 0)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })

	cfg := config.Default()
	cfg.Compaction.L5Threshold = 5
	cfg.Compaction.L5ClusterThreshold = 0.5
	cfg.Compaction.L5MinClusterSize = 2
	cfg.Compaction.SeedMinL2 = 3
	cfg.Compaction.SeedMinL3 = 5
	cfg.Compaction.SeedSample = 50 // max clusters per run
	cfg.LLM.TimeoutS = 5

	mock := &mockLLM{response: llmResp, err: llmErr}
	// Use similarEmbedder so memories cluster together for seeding tests.
	emb := &similarEmbedder{dims: 768}
	g := graph.New(d)
	store, err := memory.NewMemoryStore(d, emb, mock, g, cfg)
	if err != nil {
		t.Fatalf("NewMemoryStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	c := New(store, mock, g, cfg, slog.Default())
	return c, store
}

// TestCompactSeedNoOp verifies compactSeed is a no-op when L2 and L3 layers are already full.
func TestCompactSeedNoOp(t *testing.T) {
	c, store := seedTestSetup(t, `{"rules":[],"skills":[]}`, nil)
	ctx := context.Background()

	// Pre-populate L2 and L3 above thresholds.
	for i := 0; i < c.cfg.Compaction.SeedMinL2; i++ {
		if _, err := store.Store(ctx, 2, fmt.Sprintf("behavioral rule %d", i), nil); err != nil {
			t.Fatalf("Store L2: %v", err)
		}
	}
	for i := 0; i < c.cfg.Compaction.SeedMinL3; i++ {
		if _, err := store.Store(ctx, 3, fmt.Sprintf("skill %d", i), nil); err != nil {
			t.Fatalf("Store L3: %v", err)
		}
	}
	// Add enough L5 to pass threshold.
	for i := 0; i < c.cfg.Compaction.L5Threshold+1; i++ {
		if _, err := store.Store(ctx, 5, fmt.Sprintf("observation %d", i), nil); err != nil {
			t.Fatalf("Store L5: %v", err)
		}
	}

	l2n, l3n, err := c.compactSeed(ctx)
	if err != nil {
		t.Fatalf("compactSeed: %v", err)
	}
	if l2n != 0 || l3n != 0 {
		t.Errorf("compactSeed no-op: got l2=%d l3=%d, want 0 0", l2n, l3n)
	}
}

// TestCompactSeedL5BelowThreshold verifies compactSeed is a no-op when L5 is below threshold.
func TestCompactSeedL5BelowThreshold(t *testing.T) {
	c, store := seedTestSetup(t, `{"rules":["always handle errors"],"skills":[{"content":"debugging","tags":"go"}]}`, nil)
	ctx := context.Background()

	// L5 count below threshold — seeding must not run.
	for i := 0; i < c.cfg.Compaction.L5Threshold-1; i++ {
		if _, err := store.Store(ctx, 5, fmt.Sprintf("obs %d", i), nil); err != nil {
			t.Fatalf("Store L5: %v", err)
		}
	}

	l2n, l3n, err := c.compactSeed(ctx)
	if err != nil {
		t.Fatalf("compactSeed: %v", err)
	}
	if l2n != 0 || l3n != 0 {
		t.Errorf("expected no seeding below L5 threshold, got l2=%d l3=%d", l2n, l3n)
	}
}

// TestCompactSeedStoresL2AndL3 verifies that seeding creates L2 rules and L3 skills
// when both layers are sparse and L5 count meets the threshold. Uses similarEmbedder
// so L5 memories form a cluster that drives seeding.
func TestCompactSeedStoresL2AndL3(t *testing.T) {
	validJSON := `{"rules":["always handle errors explicitly","prefer composition over inheritance"],"skills":[{"content":"Go error wrapping","tags":"go,errors"},{"content":"Interface design","tags":"go,design"}]}`
	c, store := seedTestSetup(t, validJSON, nil)
	ctx := context.Background()

	// Insert L5 above threshold; similarEmbedder ensures they cluster.
	for i := 0; i < c.cfg.Compaction.L5Threshold+1; i++ {
		if _, err := store.Store(ctx, 5, fmt.Sprintf("raw obs %d about go development", i), nil); err != nil {
			t.Fatalf("Store L5: %v", err)
		}
	}

	l2n, l3n, err := c.compactSeed(ctx)
	if err != nil {
		t.Fatalf("compactSeed: %v", err)
	}
	if l2n == 0 {
		t.Error("expected L2 rules to be seeded, got 0")
	}
	if l3n == 0 {
		t.Error("expected L3 skills to be seeded, got 0")
	}

	// Verify actual DB counts.
	stats, err := store.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.ByLayer[2] != l2n {
		t.Errorf("L2 DB count = %d, want %d", stats.ByLayer[2], l2n)
	}
	if stats.ByLayer[3] != l3n {
		t.Errorf("L3 DB count = %d, want %d", stats.ByLayer[3], l3n)
	}
}

// TestCompactSeedLLMFailure verifies that an LLM error causes a warn-not-crash.
func TestCompactSeedLLMFailure(t *testing.T) {
	c, store := seedTestSetup(t, "", fmt.Errorf("LLM unavailable"))
	ctx := context.Background()

	for i := 0; i < c.cfg.Compaction.L5Threshold+1; i++ {
		if _, err := store.Store(ctx, 5, fmt.Sprintf("obs %d", i), nil); err != nil {
			t.Fatalf("Store L5: %v", err)
		}
	}

	// Must not return an error — failures are logged and skipped.
	l2n, l3n, err := c.compactSeed(ctx)
	if err != nil {
		t.Fatalf("compactSeed must not return top-level error on LLM failure: %v", err)
	}
	if l2n != 0 || l3n != 0 {
		t.Errorf("expected 0 seeded on LLM failure, got l2=%d l3=%d", l2n, l3n)
	}
}

// TestCompactSeedMalformedJSON verifies that malformed LLM output is skipped gracefully.
func TestCompactSeedMalformedJSON(t *testing.T) {
	c, store := seedTestSetup(t, "not valid json at all", nil)
	ctx := context.Background()

	for i := 0; i < c.cfg.Compaction.L5Threshold+1; i++ {
		if _, err := store.Store(ctx, 5, fmt.Sprintf("obs %d", i), nil); err != nil {
			t.Fatalf("Store L5: %v", err)
		}
	}

	l2n, l3n, err := c.compactSeed(ctx)
	if err != nil {
		t.Fatalf("compactSeed must not propagate parse errors: %v", err)
	}
	if l2n != 0 || l3n != 0 {
		t.Errorf("expected 0 seeded on JSON parse failure, got l2=%d l3=%d", l2n, l3n)
	}
}

// TestCompactSeedStopsWhenLayersFull verifies that seeding stops as soon as L2 and L3
// reach their target counts (i.e., SeedSample cluster cap is respected).
func TestCompactSeedStopsWhenLayersFull(t *testing.T) {
	validJSON := `{"rules":["rule A","rule B","rule C","rule D"],"skills":[{"content":"skill A","tags":"go"},{"content":"skill B","tags":"go"},{"content":"skill C","tags":"go"},{"content":"skill D","tags":"go"},{"content":"skill E","tags":"go"},{"content":"skill F","tags":"go"}]}`
	c, store := seedTestSetup(t, validJSON, nil)
	ctx := context.Background()

	// Set thresholds low so they fill fast.
	c.cfg.Compaction.SeedMinL2 = 2
	c.cfg.Compaction.SeedMinL3 = 3

	for i := 0; i < c.cfg.Compaction.L5Threshold+1; i++ {
		if _, err := store.Store(ctx, 5, fmt.Sprintf("obs %d", i), nil); err != nil {
			t.Fatalf("Store L5: %v", err)
		}
	}

	_, _, err := c.compactSeed(ctx)
	if err != nil {
		t.Fatalf("compactSeed: %v", err)
	}

	stats, err := store.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.ByLayer[2] < c.cfg.Compaction.SeedMinL2 {
		t.Errorf("L2 seeded = %d, want >= %d", stats.ByLayer[2], c.cfg.Compaction.SeedMinL2)
	}
	if stats.ByLayer[3] < c.cfg.Compaction.SeedMinL3 {
		t.Errorf("L3 seeded = %d, want >= %d", stats.ByLayer[3], c.cfg.Compaction.SeedMinL3)
	}
}

// TestCompactSeedSampleCapClusters verifies that compactSeed processes at most SeedSample clusters.
// It uses SeedSample=1 and SeedMinL2/L3 large enough that seeding won't stop early on filling.
// After the run, at most 1 cluster's worth of L2/L3 output should be stored.
func TestCompactSeedSampleCapClusters(t *testing.T) {
	// LLM returns 3 rules and 3 skills per batch — so 1 cluster cap → at most 3+3.
	oneClusterJSON := `{"rules":["rule A","rule B","rule C"],"skills":[{"content":"skill A","tags":"go"},{"content":"skill B","tags":"go"},{"content":"skill C","tags":"go"}]}`

	dir := t.TempDir()
	d, err := db.Open(filepath.Join(dir, "cap_test.db"), 0)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })

	cfg := config.Default()
	cfg.Compaction.L5Threshold = 5
	cfg.Compaction.L5ClusterThreshold = 0.5
	cfg.Compaction.L5MinClusterSize = 2
	cfg.Compaction.SeedMinL2 = 1000 // very high — won't fill naturally within one batch
	cfg.Compaction.SeedMinL3 = 1000
	cfg.Compaction.SeedSample = 1 // process exactly 1 cluster
	cfg.LLM.TimeoutS = 5

	mock := &mockLLM{response: oneClusterJSON}
	emb := &similarEmbedder{dims: 768}
	g := graph.New(d)
	store, err := memory.NewMemoryStore(d, emb, mock, g, cfg)
	if err != nil {
		t.Fatalf("NewMemoryStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	c := New(store, mock, g, cfg, slog.Default())
	ctx := context.Background()

	// Insert many L5 — similarEmbedder makes them all one cluster.
	for i := 0; i < 20; i++ {
		if _, err := store.Store(ctx, 5, fmt.Sprintf("obs %d", i), nil); err != nil {
			t.Fatalf("Store L5: %v", err)
		}
	}
	// Wait for async store goroutines before compactSeed.
	store.Close()

	l2n, l3n, err := c.compactSeed(ctx)
	if err != nil {
		t.Fatalf("compactSeed: %v", err)
	}

	// With SeedSample=1, only 1 LLM call is made → at most 3 rules + 3 skills stored.
	const maxPerBatch = 6
	if l2n+l3n > maxPerBatch {
		t.Errorf("SeedSample=1: got l2=%d l3=%d (total %d), want <= %d", l2n, l3n, l2n+l3n, maxPerBatch)
	}
}

// TestParseSeedResponse verifies JSON parsing including markdown code-fence stripping.
func TestParseSeedResponse(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantRules int
		wantErr   bool
	}{
		{
			name:      "plain JSON",
			input:     `{"rules":["rule1"],"skills":[{"content":"s","tags":"t"}]}`,
			wantRules: 1,
		},
		{
			name:      "json code fence",
			input:     "```json\n{\"rules\":[\"r\"],\"skills\":[]}\n```",
			wantRules: 1,
		},
		{
			name:      "generic code fence",
			input:     "```\n{\"rules\":[],\"skills\":[]}\n```",
			wantRules: 0,
		},
		{
			name:    "invalid JSON",
			input:   "not json",
			wantErr: true,
		},
		{
			name:      "empty rules and skills",
			input:     `{"rules":[],"skills":[]}`,
			wantRules: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sr, err := parseSeedResponse(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(sr.Rules) != tt.wantRules {
				t.Errorf("Rules count = %d, want %d", len(sr.Rules), tt.wantRules)
			}
		})
	}
}

// TestBuildSeedPrompt verifies prompt construction contains observation content.
func TestBuildSeedPrompt(t *testing.T) {
	batch := []*memory.Memory{
		{Content: "worked on rate limiter"},
		{Content: "fixed goroutine leak"},
	}
	prompt := buildSeedPrompt(batch, true, true)

	if prompt == "" {
		t.Fatal("buildSeedPrompt returned empty string")
	}
	if len(prompt) < 20 {
		t.Errorf("prompt too short: %d chars", len(prompt))
	}
	// Both contents must appear (possibly truncated but present for short strings).
	for _, m := range batch {
		substr := m.Content
		if len(substr) > 10 {
			substr = substr[:10]
		}
		found := false
		for i := 0; i <= len(prompt)-len(substr); i++ {
			if prompt[i:i+len(substr)] == substr {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("prompt does not contain observation substring %q", substr)
		}
	}
}

// TestRunOnceIncludesSeedCounts verifies that RunOnce populates report.SeedL2 and report.SeedL3.
func TestRunOnceIncludesSeedCounts(t *testing.T) {
	validJSON := `{"rules":["handle errors","test first"],"skills":[{"content":"Go concurrency","tags":"go,concurrency"}]}`
	c, store := seedTestSetup(t, validJSON, nil)
	ctx := context.Background()

	// Insert enough L5 to trigger seeding.
	for i := 0; i < c.cfg.Compaction.L5Threshold+1; i++ {
		if _, err := store.Store(ctx, 5, fmt.Sprintf("raw obs %d", i), nil); err != nil {
			t.Fatalf("Store L5: %v", err)
		}
	}

	report, err := c.RunOnce(ctx)
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if report.SeedL2 == 0 && report.SeedL3 == 0 {
		t.Error("RunOnce: expected at least one seeded memory (SeedL2 or SeedL3 > 0)")
	}
}

// TestCompactSeedDoesNotDeleteL5 verifies that seed compaction never soft-deletes L5 memories.
func TestCompactSeedDoesNotDeleteL5(t *testing.T) {
	validJSON := `{"rules":["rule1"],"skills":[{"content":"s","tags":"t"}]}`
	c, store := seedTestSetup(t, validJSON, nil)
	ctx := context.Background()

	inserted := c.cfg.Compaction.L5Threshold + 1
	for i := 0; i < inserted; i++ {
		if _, err := store.Store(ctx, 5, fmt.Sprintf("obs %d", i), nil); err != nil {
			t.Fatalf("Store L5: %v", err)
		}
	}

	_, _, err := c.compactSeed(ctx)
	if err != nil {
		t.Fatalf("compactSeed: %v", err)
	}

	stats, err := store.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.ByLayer[5] != inserted {
		t.Errorf("L5 count after seed = %d, want %d (seeding must not delete L5)", stats.ByLayer[5], inserted)
	}
}

// TestCompactSeedDoesNotReprocessConsumedL5Cluster regression-tests the fix
// for a bug found by adversarial review: compactSeed had no entity-ID guard
// (unlike compactL3toL2's derived_from Neighbors check) against re-selecting
// the same L5 cluster on a later tick while SeedMinL2/SeedMinL3 remain
// unmet -- producing duplicate-in-substance L2/L3 memories each time, since
// the LLM's differently-worded output on each call defeats content-hash
// dedup. With only one L5 cluster available and thresholds set so a single
// batch never satisfies them, a second compactSeed call must find nothing
// left to process.
func TestCompactSeedDoesNotReprocessConsumedL5Cluster(t *testing.T) {
	validJSON := `{"rules":["always handle errors explicitly","prefer composition over inheritance"],"skills":[{"content":"Go error wrapping","tags":"go,errors"},{"content":"Interface design","tags":"go,design"}]}`
	c, store := seedTestSetup(t, validJSON, nil)
	ctx := context.Background()

	// Insert exactly enough L5 to form ONE cluster; similarEmbedder ensures
	// they cluster together. seedTestSetup's SeedMinL2=3/SeedMinL3=5 are
	// never satisfied by this response's 2 rules + 2 skills, so needsL2/
	// needsL3 remain true after the first call -- if the same cluster were
	// eligible again, a second call would reprocess it.
	for i := 0; i < c.cfg.Compaction.L5Threshold+1; i++ {
		if _, err := store.Store(ctx, 5, fmt.Sprintf("raw obs %d about go development", i), nil); err != nil {
			t.Fatalf("Store L5: %v", err)
		}
	}

	l2n1, l3n1, err := c.compactSeed(ctx)
	if err != nil {
		t.Fatalf("compactSeed[1]: %v", err)
	}
	if l2n1 == 0 && l3n1 == 0 {
		t.Fatal("first compactSeed call should have seeded something (test setup issue, not the fix under test)")
	}

	l2n2, l3n2, err := c.compactSeed(ctx)
	if err != nil {
		t.Fatalf("compactSeed[2]: %v", err)
	}
	if l2n2 != 0 || l3n2 != 0 {
		t.Errorf("second compactSeed call reprocessed the already-consumed L5 cluster, got l2=%d l3=%d, want 0 0", l2n2, l3n2)
	}

	// Verify the DB wasn't quietly given duplicate-in-substance memories --
	// total L2/L3 counts must equal exactly what the first call produced.
	stats, err := store.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.ByLayer[2] != l2n1 {
		t.Errorf("L2 DB count = %d, want %d (from the first call only)", stats.ByLayer[2], l2n1)
	}
	if stats.ByLayer[3] != l3n1 {
		t.Errorf("L3 DB count = %d, want %d (from the first call only)", stats.ByLayer[3], l3n1)
	}
}

// TestCompactSeedNoL5Clusters verifies compactSeed is a no-op when no clusters form
// (memories are all dissimilar under a high threshold).
func TestCompactSeedNoL5Clusters(t *testing.T) {
	validJSON := `{"rules":["r"],"skills":[{"content":"s","tags":"t"}]}`

	dir := t.TempDir()
	d, err := db.Open(filepath.Join(dir, "noclusters.db"), 0)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })

	cfg := config.Default()
	cfg.Compaction.L5Threshold = 2
	cfg.Compaction.L5ClusterThreshold = 0.9999 // near-1 threshold — only identical vectors cluster
	cfg.Compaction.L5MinClusterSize = 2
	cfg.Compaction.SeedMinL2 = 10
	cfg.Compaction.SeedMinL3 = 20
	cfg.Compaction.SeedSample = 50
	cfg.LLM.TimeoutS = 5

	mock := &mockLLM{response: validJSON}
	// Use mockEmbedder (hash-based, 768 dims) so memories get dissimilar embeddings.
	emb := &mockEmbedder{dims: 768}
	g := graph.New(d)
	store, err := memory.NewMemoryStore(d, emb, mock, g, cfg)
	if err != nil {
		t.Fatalf("NewMemoryStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	c := New(store, mock, g, cfg, slog.Default())
	ctx := context.Background()

	// Insert L5 above threshold.
	for i := 0; i < cfg.Compaction.L5Threshold+1; i++ {
		content := fmt.Sprintf("completely unique topic %d xyz abc def", i)
		if _, err := store.Store(ctx, 5, content, nil); err != nil {
			t.Fatalf("Store L5: %v", err)
		}
	}

	l2n, l3n, err := c.compactSeed(ctx)
	if err != nil {
		t.Fatalf("compactSeed: %v", err)
	}
	// With no clusters, nothing should be seeded.
	if l2n != 0 || l3n != 0 {
		t.Errorf("expected 0 seeded when no clusters form, got l2=%d l3=%d", l2n, l3n)
	}
}
