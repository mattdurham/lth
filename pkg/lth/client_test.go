// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package lth

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/mattdurham/lth/internal/config"
)

// testClient creates a Client with a temp DB for testing.
// It requires no network (Ollama) - the embedder will fail gracefully.
func testClient(t *testing.T) *Client {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DB.Path = filepath.Join(dir, "test.db")
	// Use a non-existent embedder URL so embedding fails gracefully (store without embedding).
	cfg.Embedding.BaseURL = "http://localhost:0"
	cfg.LLM.BaseURL = "http://localhost:0"

	c, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestNewClientClose(t *testing.T) {
	c := testClient(t)
	if c == nil {
		t.Fatal("NewClient returned nil")
	}
	// Close is called by t.Cleanup in testClient.
}

func TestStoreAndGet(t *testing.T) {
	ctx := context.Background()
	c := testClient(t)

	m, err := c.Store(ctx, 5, "test content for get", nil)
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	if m == nil {
		t.Fatal("Store returned nil memory")
	}

	got, err := c.Get(ctx, m.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Content != "test content for get" {
		t.Errorf("Content = %q, want %q", got.Content, "test content for get")
	}
	if got.Layer != 5 {
		t.Errorf("Layer = %d, want 5", got.Layer)
	}
}

func TestStats(t *testing.T) {
	ctx := context.Background()
	c := testClient(t)

	if _, err := c.Store(ctx, 3, "L3 memory content", nil); err != nil {
		t.Fatalf("Store L3: %v", err)
	}
	if _, err := c.Store(ctx, 5, "L5 memory content", nil); err != nil {
		t.Fatalf("Store L5: %v", err)
	}

	stats, err := c.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.TotalMemories != 2 {
		t.Errorf("TotalMemories = %d, want 2", stats.TotalMemories)
	}
	if stats.ByLayer[3] != 1 {
		t.Errorf("ByLayer[3] = %d, want 1", stats.ByLayer[3])
	}
	if stats.ByLayer[5] != 1 {
		t.Errorf("ByLayer[5] = %d, want 1", stats.ByLayer[5])
	}
}
