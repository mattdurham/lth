// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestComplete_success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method %q", r.Method)
		}

		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message": map[string]string{
						"content": "7",
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	llm := NewOllamaLLM(srv.URL, "test-model", 30)
	got, err := llm.Complete(context.Background(), "rate this 1-10")
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if got != "7" {
		t.Errorf("Complete = %q, want 7", got)
	}
}

func TestComplete_serverError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	llm := NewOllamaLLM(srv.URL, "test-model", 30)
	_, err := llm.Complete(context.Background(), "rate this 1-10")
	if err == nil {
		t.Error("Complete: expected error for 500 status, got nil")
	}
}

func TestComplete_timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Block for longer than the context deadline.
		select {
		case <-r.Context().Done():
			http.Error(w, "canceled", http.StatusServiceUnavailable)
		case <-time.After(2 * time.Second):
			http.Error(w, "slow", http.StatusGatewayTimeout)
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	llm := NewOllamaLLM(srv.URL, "test-model", 30)
	_, err := llm.Complete(ctx, "rate this 1-10")
	if err == nil {
		t.Error("Complete: expected error for timeout, got nil")
	}
}
