// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package issueswatcher

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
// irrelevant to this test.
type fakeEmbedder struct{ dims int }

func (f *fakeEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	return make([]float32, f.dims), nil
}
func (f *fakeEmbedder) Dims() int { return f.dims }

// fakeLLM returns a fixed response regardless of prompt; used only for the
// memory store's async importance/tag/valence scoring, whose responses this
// test never inspects.
type fakeLLM struct{}

func (fakeLLM) Complete(_ context.Context, _ string) (string, error) { return "5", nil }

func testStore(t *testing.T) *memory.MemoryStore {
	t.Helper()
	dir := t.TempDir()
	d, err := db.Open(filepath.Join(dir, "test.db"), 0)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })

	g := graph.New(d)
	store, err := memory.NewMemoryStore(d, &fakeEmbedder{dims: 8}, fakeLLM{}, g, config.Default())
	if err != nil {
		t.Fatalf("NewMemoryStore: %v", err)
	}
	t.Cleanup(store.Close)
	return store
}

// TestProcessIssuePersistsStateImmediately regression-tests the fix for
// issueswatcher's sibling of the prwatcher/mdwatcher batch-persist-at-end
// bug: state must be on disk as soon as one issue's Store() completes, not
// only after syncRepo's entire batch finishes. Uses Comments: 0 so the test
// never needs to shell out to `gh` for comments.
func TestProcessIssuePersistsStateImmediately(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)

	dir := t.TempDir()
	w := New(store, config.Default(), nil)
	w.stateFile = filepath.Join(dir, "issues-state.json")
	w.st = state{Repos: map[string]repoState{}}

	issue := ghIssue{
		Number:    42,
		Title:     "Something broke",
		Body:      "Steps to reproduce...",
		State:     "open",
		HTMLURL:   "https://github.com/acme/widgets/issues/42",
		UpdatedAt: "2026-01-01T00:00:00Z",
		Comments:  0,
	}

	if err := w.processIssue(ctx, "acme/widgets", issue); err != nil {
		t.Fatalf("processIssue: %v", err)
	}

	// The whole point of the fix: state must be on disk immediately after
	// processIssue returns, before syncRepo's end-of-batch save (which this
	// test never calls) would otherwise be the only save.
	data, err := os.ReadFile(w.stateFile)
	if err != nil {
		t.Fatalf("state file should exist immediately after processIssue, got: %v", err)
	}
	var persisted state
	if err := json.Unmarshal(data, &persisted); err != nil {
		t.Fatalf("unmarshal persisted state: %v", err)
	}
	is, ok := persisted.Repos["acme/widgets"].Issues["42"]
	if !ok {
		t.Fatalf("persisted state missing issue 42")
	}
	if is.MemoryID == "" {
		t.Errorf("persisted issueState has no MemoryID: %+v", is)
	}
	if is.UpdatedAt != issue.UpdatedAt {
		t.Errorf("persisted UpdatedAt = %q, want %q", is.UpdatedAt, issue.UpdatedAt)
	}
}

// TestProcessIssueSkipsUnchangedIssue confirms the incremental save doesn't
// break the existing unchanged-issue short-circuit.
func TestProcessIssueSkipsUnchangedIssue(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)

	dir := t.TempDir()
	w := New(store, config.Default(), nil)
	w.stateFile = filepath.Join(dir, "issues-state.json")
	w.st = state{Repos: map[string]repoState{}}

	issue := ghIssue{
		Number:    7,
		Title:     "Unchanged",
		State:     "open",
		HTMLURL:   "https://github.com/acme/widgets/issues/7",
		UpdatedAt: "2026-01-01T00:00:00Z",
		Comments:  0,
	}

	if err := w.processIssue(ctx, "acme/widgets", issue); err != nil {
		t.Fatalf("processIssue[1]: %v", err)
	}
	first := w.st.Repos["acme/widgets"].Issues["7"].MemoryID

	if err := w.processIssue(ctx, "acme/widgets", issue); err != nil {
		t.Fatalf("processIssue[2]: %v", err)
	}
	second := w.st.Repos["acme/widgets"].Issues["7"].MemoryID

	if first != second {
		t.Errorf("re-processing an unchanged issue should be a no-op, got MemoryID %q then %q", first, second)
	}
}
