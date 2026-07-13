// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package mdwatcher

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/mattdurham/lth/internal/config"
	"github.com/mattdurham/lth/internal/db"
	"github.com/mattdurham/lth/internal/graph"
	"github.com/mattdurham/lth/internal/memory"
)

// fakeEmbedder returns a fixed-size zero vector; embedding quality is
// irrelevant to these tests.
type fakeEmbedder struct{ dims int }

func (f *fakeEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	return make([]float32, f.dims), nil
}
func (f *fakeEmbedder) Dims() int { return f.dims }

// fakeLLM returns a fixed response regardless of prompt; used both for
// mdwatcher's fact-extraction call and the memory store's async
// importance/tag/valence scoring (whose responses this test never inspects).
type fakeLLM struct{ response string }

func (f *fakeLLM) Complete(_ context.Context, _ string) (string, error) {
	return f.response, nil
}

func testStoreAndWatcher(t *testing.T, facts []string) (*MDWatcher, string) {
	t.Helper()
	dir := t.TempDir()

	d, err := db.Open(filepath.Join(dir, "test.db"), 0)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })

	g := graph.New(d)
	factsJSON, err := json.Marshal(facts)
	if err != nil {
		t.Fatalf("marshal facts: %v", err)
	}
	factLLM := &fakeLLM{response: string(factsJSON)}

	store, err := memory.NewMemoryStore(d, &fakeEmbedder{dims: 8}, factLLM, g, config.Default())
	if err != nil {
		t.Fatalf("NewMemoryStore: %v", err)
	}
	t.Cleanup(store.Close)

	cfg := config.Default()
	cfg.Markdown.Layer = 5
	w := New(store, factLLM, cfg, nil)
	w.stateFile = filepath.Join(dir, "mdwatcher-state.json")
	w.st = state{Files: map[string]fileState{}}

	return w, dir
}

// TestProcessFilePersistsStateImmediately regression-tests the CRITICAL fix
// for mdwatcher's sibling of the prwatcher batch-persist-at-end bug: state
// must be on disk as soon as one file's ingestion completes, not only after
// ScanOnce's entire batch finishes, because the ingested content is
// LLM-generated (non-deterministic) and so is not protected by content-hash
// dedup on a re-ingest.
func TestProcessFilePersistsStateImmediately(t *testing.T) {
	ctx := context.Background()
	w, dir := testStoreAndWatcher(t, []string{"fact one", "fact two"})

	mdPath := filepath.Join(dir, "notes.md")
	if err := os.WriteFile(mdPath, []byte("# Notes\nsome content"), 0o600); err != nil {
		t.Fatalf("write md: %v", err)
	}

	if err := w.processFile(ctx, mdPath); err != nil {
		t.Fatalf("processFile: %v", err)
	}

	// The whole point of the fix: the state file must exist on disk
	// immediately after processFile returns, before ScanOnce's batch-level
	// save (which this test never calls) would otherwise be the only save.
	data, err := os.ReadFile(w.stateFile)
	if err != nil {
		t.Fatalf("state file should exist immediately after processFile, got: %v", err)
	}
	var persisted state
	if err := json.Unmarshal(data, &persisted); err != nil {
		t.Fatalf("unmarshal persisted state: %v", err)
	}
	fs, ok := persisted.Files[mdPath]
	if !ok {
		t.Fatalf("persisted state missing entry for %s", mdPath)
	}
	if len(fs.MemoryIDs) != 2 {
		t.Errorf("persisted MemoryIDs = %v, want 2 (one per extracted fact)", fs.MemoryIDs)
	}
}

// TestProcessFileSecondCallSkipsUnchangedFile confirms the incremental save
// doesn't break the existing unchanged-file short-circuit: re-processing the
// same file with an identical hash must not re-ingest or grow MemoryIDs.
func TestProcessFileSecondCallSkipsUnchangedFile(t *testing.T) {
	ctx := context.Background()
	w, dir := testStoreAndWatcher(t, []string{"fact one"})

	mdPath := filepath.Join(dir, "notes.md")
	if err := os.WriteFile(mdPath, []byte("# Notes\nunchanged content"), 0o600); err != nil {
		t.Fatalf("write md: %v", err)
	}

	if err := w.processFile(ctx, mdPath); err != nil {
		t.Fatalf("processFile[1]: %v", err)
	}
	firstIDs := append([]string(nil), w.st.Files[mdPath].MemoryIDs...)

	if err := w.processFile(ctx, mdPath); err != nil {
		t.Fatalf("processFile[2]: %v", err)
	}
	secondIDs := w.st.Files[mdPath].MemoryIDs

	if len(firstIDs) != len(secondIDs) {
		t.Errorf("re-processing an unchanged file should be a no-op, got %v then %v", firstIDs, secondIDs)
	}
}
