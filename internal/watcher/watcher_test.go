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
	w, err := New(store, cfg, nil)
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

	if err := w.IngestFile(context.Background(), jsonlFile); err != nil {
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
	w, err := New(store, cfg, nil)
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
	w, err := New(store, cfg, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	jsonlFile := filepath.Join(dir, "err.jsonl")
	line := `{"type":"user","message":{"role":"user","content":"will fail"},"sessionId":"s1","cwd":"/"}` + "\n"
	if err := os.WriteFile(jsonlFile, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}

	// ingestFile should not return an error even if the store fails (it logs and continues).
	if err := w.IngestFile(context.Background(), jsonlFile); err != nil {
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

	if err := w1.IngestFile(context.Background(), jsonlFile); err != nil {
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

	if err := w2.IngestFile(context.Background(), jsonlFile); err != nil {
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
	w, err := New(store, cfg, nil)
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

	if err := w.IngestFile(context.Background(), jsonlFile); err != nil {
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

// mockStoreWithAttrs tracks Store calls including attrs for detailed assertions.
type mockStoreWithAttrs struct {
	mu    sync.Mutex
	calls []mockStoreCall
}

type mockStoreCall struct {
	content string
	attrs   map[string]string
}

func (m *mockStoreWithAttrs) Store(_ context.Context, _ int, content string, attrs map[string]string) (*memory.Memory, error) {
	m.mu.Lock()
	m.calls = append(m.calls, mockStoreCall{content: content, attrs: attrs})
	m.mu.Unlock()
	return &memory.Memory{Content: content}, nil
}
func (m *mockStoreWithAttrs) Get(_ context.Context, _ string) (*memory.Memory, error) {
	return nil, nil
}
func (m *mockStoreWithAttrs) Search(_ context.Context, _ *memory.SearchRequest) ([]*memory.ScoredMemory, error) {
	return nil, nil
}
func (m *mockStoreWithAttrs) Stats(_ context.Context) (*memory.Stats, error) {
	return &memory.Stats{}, nil
}
func (m *mockStoreWithAttrs) ListLayer(_ context.Context, _ int) ([]*memory.Memory, error) {
	return nil, nil
}
func (m *mockStoreWithAttrs) SoftDelete(_ context.Context, _ []string, _ string) error { return nil }

func TestWatcherIngestsWllrFile(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Watcher.StateFile = filepath.Join(dir, "state.json")

	// Create a temp dir whose path contains "/.wllr/" so detectFormat returns FormatWllr.
	wllrDir := filepath.Join(dir, ".wllr", "sessions", "sess-abc")
	if err := os.MkdirAll(wllrDir, 0o755); err != nil {
		t.Fatalf("create wllr dir: %v", err)
	}

	cfg.Watcher.Paths = []string{wllrDir}

	store := &mockStoreWithAttrs{}
	w, err := New(store, cfg, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// wllr JSONL file: session header + 2 messages + 1 tool_call (should be skipped).
	lines := []string{
		`{"type":"session","id":"sess-abc","cwd":"/home/user/project","timestamp":"2024-01-01T00:00:00Z"}`,
		`{"type":"message","id":"m1","role":"user","content":"what is lth?","timestamp":"2024-01-01T00:01:00Z"}`,
		`{"type":"message","id":"m2","role":"assistant","content":"lth is a memory system","timestamp":"2024-01-01T00:02:00Z"}`,
		`{"type":"tool_call","id":"t1","tool_name":"exec","timestamp":"2024-01-01T00:03:00Z"}`,
	}
	jsonlContent := strings.Join(lines, "\n") + "\n"
	jsonlFile := filepath.Join(wllrDir, "session.jsonl")
	if err := os.WriteFile(jsonlFile, []byte(jsonlContent), 0o600); err != nil {
		t.Fatalf("write wllr file: %v", err)
	}

	if err := w.IngestFile(context.Background(), jsonlFile); err != nil {
		t.Fatalf("ingestFile: %v", err)
	}

	store.mu.Lock()
	calls := append([]mockStoreCall(nil), store.calls...)
	store.mu.Unlock()

	// Expect exactly 2 Store calls: user message + assistant message.
	// Session line and tool_call line must NOT produce memories.
	if len(calls) != 2 {
		t.Fatalf("Store called %d times, want 2; calls: %v", len(calls), calls)
	}

	// Verify contents.
	contents := map[string]bool{}
	for _, c := range calls {
		contents[c.content] = true
	}
	if !contents["what is lth?"] {
		t.Errorf("expected Store call with content %q; got %v", "what is lth?", calls)
	}
	if !contents["lth is a memory system"] {
		t.Errorf("expected Store call with content %q; got %v", "lth is a memory system", calls)
	}

	// Verify cwd is carried from the session header to all message memories.
	for _, c := range calls {
		if c.attrs["cwd"] != "/home/user/project" {
			t.Errorf("Store call for %q: attrs[cwd] = %q, want %q", c.content, c.attrs["cwd"], "/home/user/project")
		}
	}
}
