// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package vector

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestOllamaEmbedder(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		want := make([]float64, 768)
		for i := range want {
			want[i] = float64(i) * 0.001
		}

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/v1/embeddings" {
				t.Errorf("unexpected path: %s", r.URL.Path)
			}
			resp := map[string]any{
				"data": []map[string]any{
					{"embedding": want},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
		}))
		defer srv.Close()

		emb := NewOllamaEmbedder(srv.URL, "nomic-embed-text", 10)
		got, err := emb.Embed(context.Background(), "hello world")
		if err != nil {
			t.Fatalf("Embed: %v", err)
		}
		if len(got) != len(want) {
			t.Fatalf("len(embedding) = %d, want %d", len(got), len(want))
		}
		for i := range want {
			if got[i] != float32(want[i]) {
				t.Errorf("embedding[%d] = %f, want %f", i, got[i], float32(want[i]))
			}
		}
		if emb.Dims() != len(want) {
			t.Errorf("Dims() = %d, want %d", emb.Dims(), len(want))
		}
	})

	t.Run("server_error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "internal error", http.StatusInternalServerError)
		}))
		defer srv.Close()

		emb := NewOllamaEmbedder(srv.URL, "nomic-embed-text", 10)
		_, err := emb.Embed(context.Background(), "test")
		if err == nil {
			t.Error("expected error on 500 response, got nil")
		}
	})

	t.Run("timeout", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			time.Sleep(100 * time.Millisecond)
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		emb := NewOllamaEmbedder(srv.URL, "nomic-embed-text", 10)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		defer cancel()

		_, err := emb.Embed(ctx, "test")
		if err == nil {
			t.Error("expected context deadline error, got nil")
		}
	})
}
