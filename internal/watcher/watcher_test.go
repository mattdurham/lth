// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package watcher

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/mattdurham/lth/internal/config"
	"github.com/mattdurham/lth/internal/memory"
)

// mockStore tracks Store calls.
type mockStore struct {
	mu      sync.Mutex
	calls   []string // contents stored
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

func TestWatcherIngestsNewLines(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Watcher.StateFile = filepath.Join(dir, "state.json")
	cfg.Watcher.Paths = []string{dir}

	store := &mockStore{}
	w, err := New(store, cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Write JSONL lines to a file.
	jsonlFile := filepath.Join(dir, "conv.jsonl")
	lines := []string{
		`{"type":"user","message":{"role":"user","content":"first message"},"sessionId":"s1","cwd":"/"}`,
		`{"type":"user","message":{"role":"user","content":"second message"},"sessionId":"s1","cwd":"/"}`,
	}
	content := ""
	for _, l := range lines {
		content += l + "\n"
	}
	if err := os.WriteFile(jsonlFile, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := w.ingestFile(context.Background(), jsonlFile); err != nil {
		t.Fatalf("ingestFile: %v", err)
	}

	store.mu.Lock()
	got := len(store.calls)
	store.mu.Unlock()

	if got != 2 {
		t.Errorf("Store called %d times, want 2", got)
	}
}

func TestWatcherInitialScan(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Watcher.StateFile = filepath.Join(dir, "state.json")
	cfg.Watcher.Paths = []string{dir}

	// Write a JSONL file BEFORE creating the watcher.
	jsonlFile := filepath.Join(dir, "preexist.jsonl")
	lines := []string{
		`{"type":"user","message":{"role":"user","content":"pre-existing line one"},"sessionId":"s1","cwd":"/"}`,
		`{"type":"user","message":{"role":"user","content":"pre-existing line two"},"sessionId":"s1","cwd":"/"}`,
		`{"type":"user","message":{"role":"user","content":"pre-existing line three"},"sessionId":"s1","cwd":"/"}`,
	}
	content := ""
	for _, l := range lines {
		content += l + "\n"
	}
	if err := os.WriteFile(jsonlFile, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	store := &mockStore{}
	w, err := New(store, cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// scanExisting mimics what Start() does on startup.
	w.scanExisting(context.Background(), dir)

	store.mu.Lock()
	got := len(store.calls)
	store.mu.Unlock()

	if got != 3 {
		t.Errorf("initial scan: Store called %d times, want 3", got)
	}
}

func TestWatcherIngestFileStoreError(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Watcher.StateFile = filepath.Join(dir, "state.json")
	cfg.Watcher.Paths = []string{dir}

	store := &mockStore{callErr: fmt.Errorf("store unavailable")}
	w, err := New(store, cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	jsonlFile := filepath.Join(dir, "err.jsonl")
	line := `{"type":"user","message":{"role":"user","content":"will fail"},"sessionId":"s1","cwd":"/"}` + "\n"
	if err := os.WriteFile(jsonlFile, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}

	// ingestFile should not return an error even if the store fails (it logs and continues).
	if err := w.ingestFile(context.Background(), jsonlFile); err != nil {
		t.Errorf("ingestFile with store error: expected nil error, got %v", err)
	}
}

func TestWatcherOffsetPersistence(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Watcher.StateFile = filepath.Join(dir, "state.json")
	cfg.Watcher.Paths = []string{dir}

	store1 := &mockStore{}
	w1, err := New(store1, cfg)
	if err != nil {
		t.Fatalf("New(w1): %v", err)
	}

	// Write 3 lines.
	jsonlFile := filepath.Join(dir, "conv.jsonl")
	initial := ""
	for i := 0; i < 3; i++ {
		initial += fmt.Sprintf(`{"type":"user","message":{"role":"user","content":"msg %d"},"sessionId":"s1","cwd":"/"}`, i) + "\n"
	}
	if err := os.WriteFile(jsonlFile, []byte(initial), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := w1.ingestFile(context.Background(), jsonlFile); err != nil {
		t.Fatalf("w1.ingestFile: %v", err)
	}

	// w1 should have stored 3 messages.
	if len(store1.calls) != 3 {
		t.Errorf("w1: stored %d, want 3", len(store1.calls))
	}

	// Wait briefly for offset save.
	time.Sleep(10 * time.Millisecond)

	// Write 2 more lines.
	additional := ""
	for i := 3; i < 5; i++ {
		additional += fmt.Sprintf(`{"type":"user","message":{"role":"user","content":"msg %d"},"sessionId":"s1","cwd":"/"}`, i) + "\n"
	}
	f, err := os.OpenFile(jsonlFile, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(additional); err != nil {
		t.Fatal(err)
	}
	f.Close() //nolint:errcheck

	// Create new watcher that loads the saved offset.
	store2 := &mockStore{}
	w2, err := New(store2, cfg)
	if err != nil {
		t.Fatalf("New(w2): %v", err)
	}

	if err := w2.ingestFile(context.Background(), jsonlFile); err != nil {
		t.Fatalf("w2.ingestFile: %v", err)
	}

	// w2 should have stored only 2 new messages (from saved offset).
	if len(store2.calls) != 2 {
		t.Errorf("w2: stored %d, want 2 (only new lines)", len(store2.calls))
	}
}
