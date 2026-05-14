// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package vector

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mattdurham/lth/internal/config"
)

func TestEnsureEmbeddingServer_autoDockerDisabled(t *testing.T) {
	cfg := &config.Config{}
	cfg.Embedding.Provider = "huggingface"
	cfg.Embedding.AutoDocker = false

	// Should return nil immediately without attempting any network calls.
	if err := EnsureEmbeddingServer(cfg); err != nil {
		t.Errorf("expected nil error when auto_docker=false, got: %v", err)
	}
}

func TestEnsureEmbeddingServer_nonHuggingfaceProvider(t *testing.T) {
	cfg := &config.Config{}
	cfg.Embedding.Provider = "ollama"
	cfg.Embedding.AutoDocker = true

	// Should return nil immediately — auto-docker is only for huggingface.
	if err := EnsureEmbeddingServer(cfg); err != nil {
		t.Errorf("expected nil error for non-huggingface provider, got: %v", err)
	}
}

func TestEnsureEmbeddingServer_serverAlreadyReachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	cfg := &config.Config{}
	cfg.Embedding.Provider = "huggingface"
	cfg.Embedding.AutoDocker = true
	cfg.Embedding.BaseURL = srv.URL

	// Server is reachable — should return nil without touching docker.
	if err := EnsureEmbeddingServer(cfg); err != nil {
		t.Errorf("expected nil when server reachable, got: %v", err)
	}
}

func TestPingEmbeddingServer_success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	if err := pingEmbeddingServer(srv.URL); err != nil {
		t.Errorf("pingEmbeddingServer: %v", err)
	}
}

func TestPingEmbeddingServer_failure(t *testing.T) {
	// Use a server that returns non-200.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	if err := pingEmbeddingServer(srv.URL); err == nil {
		t.Error("expected error for non-200 status, got nil")
	}
}

func TestPingEmbeddingServer_unreachable(t *testing.T) {
	// Use a port that is not listening.
	if err := pingEmbeddingServer("http://127.0.0.1:19999"); err == nil {
		t.Error("expected error for unreachable server, got nil")
	}
}
