package lth

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/mattdurham/lth/internal/config"
	"github.com/mattdurham/lth/internal/memory"
	"github.com/mattdurham/lth/internal/watcher"
)

// mockStore tracks Store calls.
type mockStore struct {
	mu      sync.Mutex
	calls   []string
	callErr error
}

func (m *mockStore) Store(_ context.Context, _ int, content string, _ map[string]string) (*memory.Memory, error) {
	if m.callErr != nil {
		return nil, m.callErr
	}
	m.mu.Lock()
	m.calls = append(m.calls, content)
	m.mu.Unlock()
	return &memory.Memory{Content: content, ID: fmt.Sprintf("mock-%d", len(m.calls))}, nil
}

func (m *mockStore) Get(_ context.Context, _ string) (*memory.Memory, error) { return nil, nil }
func (m *mockStore) Search(_ context.Context, _ *memory.SearchRequest) ([]*memory.ScoredMemory, error) {
	return nil, nil
}
func (m *mockStore) Stats(_ context.Context) (*memory.Stats, error)               { return &memory.Stats{}, nil }
func (m *mockStore) ListLayer(_ context.Context, _ int) ([]*memory.Memory, error) { return nil, nil }
func (m *mockStore) SoftDelete(_ context.Context, _ []string, _ string) error     { return nil }

func TestMultipleFilePathsInSingleMessage(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Watcher.StateFile = filepath.Join(dir, "state.json")
	cfg.Watcher.Paths = []string{dir}

	store := &mockStore{}
	w, err := watcher.New(store, cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// One user message + one assistant message with TWO files (Read on file1, Write on file2)
	lines := []string{
		`{"type":"user","message":{"role":"user","content":"edit two files"},"sessionId":"s1","cwd":"/"}`,
		`{"type":"assistant","sessionId":"s1","message":{"role":"assistant","content":[{"type":"tool_use","name":"Read","input":{"file_path":"/src/content/_index.md"}},{"type":"tool_use","name":"Write","input":{"file_path":"/src/content/p1/index.md"}}]}}`,
	}
	content := ""
	for _, l := range lines {
		content += l + "\n"
	}
	jsonlFile := filepath.Join(dir, "conv.jsonl")
	if err := os.WriteFile(jsonlFile, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := w.IngestFile(context.Background(), jsonlFile); err != nil {
		t.Fatalf("ingestFile: %v", err)
	}

	store.mu.Lock()
	calls := append([]string(nil), store.calls...)
	store.mu.Unlock()

	// Check what was stored
	t.Logf("Total calls to Store: %d\n", len(calls))
	for i, call := range calls {
		t.Logf("Call %d: %s\n", i+1, call)
	}

	// Should have: 1 user message + 1 files-touched memory
	var filesTouchedFound bool
	for _, c := range calls {
		if strings.Contains(c, "Files touched:") &&
			strings.Contains(c, "/src/content/_index.md") &&
			strings.Contains(c, "/src/content/p1/index.md") {
			filesTouchedFound = true
			t.Logf("\nBOTH FILES FOUND in files-touched memory!\n")
		}
	}

	if !filesTouchedFound {
		t.Errorf("expected a 'Files touched' memory with BOTH files to be stored; got calls: %v", calls)
	}
}
