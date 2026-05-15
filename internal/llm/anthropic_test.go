// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func TestAnthropicComplete_success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method %q", r.Method)
		}
		if r.Header.Get("x-api-key") != "test-key" {
			t.Errorf("missing or wrong x-api-key header: %q", r.Header.Get("x-api-key"))
		}
		if r.Header.Get("anthropic-version") == "" {
			t.Error("missing anthropic-version header")
		}

		resp := map[string]interface{}{
			"content": []map[string]interface{}{
				{"type": "text", "text": "8"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	// Override base URL for testing by constructing directly.
	a := &AnthropicLLM{
		apiKey:  "test-key",
		model:   "claude-test",
		client:  &http.Client{Timeout: 30 * time.Second},
		baseURL: srv.URL,
	}

	got, err := a.Complete(context.Background(), "rate this 1-10")
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if got != "8" {
		t.Errorf("Complete = %q, want 8", got)
	}
}

func TestAnthropicComplete_serverError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	a := &AnthropicLLM{
		apiKey:  "test-key",
		model:   "claude-test",
		client:  &http.Client{Timeout: 30 * time.Second},
		baseURL: srv.URL,
	}

	_, err := a.Complete(context.Background(), "rate this 1-10")
	if err == nil {
		t.Error("Complete: expected error for 500 status, got nil")
	}
}

func TestAnthropicComplete_timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

	a := &AnthropicLLM{
		apiKey:  "test-key",
		model:   "claude-test",
		client:  &http.Client{Timeout: 30 * time.Second},
		baseURL: srv.URL,
	}

	_, err := a.Complete(ctx, "rate this 1-10")
	if err == nil {
		t.Error("Complete: expected error for timeout, got nil")
	}
}

func TestAnthropicComplete_envAPIKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "env-key" {
			t.Errorf("wrong x-api-key: %q", r.Header.Get("x-api-key"))
		}
		resp := map[string]interface{}{
			"content": []map[string]interface{}{
				{"type": "text", "text": "5"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	t.Setenv("ANTHROPIC_API_KEY", "env-key")

	a := &AnthropicLLM{
		apiKey:  "", // empty; should fall back to env var
		model:   "claude-test",
		client:  &http.Client{Timeout: 30 * time.Second},
		baseURL: srv.URL,
	}

	got, err := a.Complete(context.Background(), "test")
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if got != "5" {
		t.Errorf("got %q, want 5", got)
	}
}

func TestAnthropicComplete_noContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]interface{}{
			"content": []map[string]interface{}{},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	a := &AnthropicLLM{
		apiKey:  "test-key",
		model:   "claude-test",
		client:  &http.Client{Timeout: 30 * time.Second},
		baseURL: srv.URL,
	}

	_, err := a.Complete(context.Background(), "test")
	if err == nil {
		t.Error("expected error for empty content, got nil")
	}
}

func TestNewAnthropicLLM(t *testing.T) {
	_ = os.Unsetenv("ANTHROPIC_API_KEY")

	a := NewAnthropicLLM("my-key", "claude-haiku-4-5-20251001", 30)
	if a == nil {
		t.Fatal("NewAnthropicLLM returned nil")
	}
	if a.model != "claude-haiku-4-5-20251001" {
		t.Errorf("model = %q, want claude-haiku-4-5-20251001", a.model)
	}
	if a.apiKey != "my-key" {
		t.Errorf("apiKey = %q, want my-key", a.apiKey)
	}
}
