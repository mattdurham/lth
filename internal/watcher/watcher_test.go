// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package watcher

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

func TestRepoForPath(t *testing.T) {
	dir := t.TempDir()

	// Create a fake git repo with go.mod.
	gitDir := filepath.Join(dir, "myrepo")
	if err := os.MkdirAll(filepath.Join(gitDir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	gomod := "module github.com/example/myrepo\n\ngo 1.22\n"
	if err := os.WriteFile(filepath.Join(gitDir, "go.mod"), []byte(gomod), 0o600); err != nil {
		t.Fatal(err)
	}
	subDir := filepath.Join(gitDir, "internal", "foo")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}

	filePath := filepath.Join(subDir, "bar.go")
	got := RepoForPath(filePath)
	if got != "github.com/example/myrepo" {
		t.Errorf("RepoForPath = %q, want %q", got, "github.com/example/myrepo")
	}
}

func TestRepoForPath_NoGitRoot(t *testing.T) {
	// A path with no .git ancestor should return "".
	dir := t.TempDir()
	got := RepoForPath(filepath.Join(dir, "file.go"))
	if got != "" {
		t.Errorf("RepoForPath = %q, want empty", got)
	}
}

func TestIngestFileStoresFilesTouched(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Watcher.StateFile = filepath.Join(dir, "state.json")
	cfg.Watcher.Paths = []string{dir}

	store := &mockStore{}
	w, err := New(store, cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// One user message + one assistant message with Read and Write tool_use blocks.
	lines := []string{
		`{"type":"user","message":{"role":"user","content":"do the thing"},"sessionId":"s1","cwd":"/"}`,
		`{"type":"assistant","sessionId":"s1","message":{"role":"assistant","content":[{"type":"tool_use","name":"Read","input":{"file_path":"/src/a.go"}},{"type":"tool_use","name":"Write","input":{"file_path":"/src/b.go"}}]}}`,
	}
	content := ""
	for _, l := range lines {
		content += l + "\n"
	}
	jsonlFile := filepath.Join(dir, "conv.jsonl")
	if err := os.WriteFile(jsonlFile, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := w.ingestFile(context.Background(), jsonlFile); err != nil {
		t.Fatalf("ingestFile: %v", err)
	}

	store.mu.Lock()
	calls := append([]string(nil), store.calls...)
	store.mu.Unlock()

	// Expect: 1 content memory (user message) + 1 files-touched memory.
	// The assistant message has no text content so ParseLine skips it;
	// but ParseFilePaths extracts paths and storeFilesTouched fires.
	var filesTouchedFound bool
	for _, c := range calls {
		if strings.Contains(c, "Files touched:") &&
			strings.Contains(c, "/src/a.go") &&
			strings.Contains(c, "/src/b.go") {
			filesTouchedFound = true
		}
	}
	if !filesTouchedFound {
		t.Errorf("expected a 'Files touched' memory to be stored; got calls: %v", calls)
	}
}
