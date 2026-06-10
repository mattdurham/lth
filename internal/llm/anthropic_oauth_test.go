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

type staticTokenSource struct{ tok string }

func (s staticTokenSource) AccessToken(_ context.Context) (string, error) { return s.tok, nil }

// TestAnthropic_oauthHeaders verifies that when a TokenSource is set, the
// client uses Authorization: Bearer + Claude Code identity headers instead
// of x-api-key.
func TestAnthropic_oauthHeaders(t *testing.T) {
	var gotAuth, gotAPIKey, gotBeta, gotUA, gotXApp string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotAPIKey = r.Header.Get("x-api-key")
		gotBeta = r.Header.Get("anthropic-beta")
		gotUA = r.Header.Get("user-agent")
		gotXApp = r.Header.Get("x-app")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"content": []map[string]any{{"type": "text", "text": "ok"}},
		})
	}))
	defer srv.Close()

	a := &AnthropicLLM{
		model:   "claude-test",
		client:  &http.Client{Timeout: 5 * time.Second},
		baseURL: srv.URL,
	}
	a.SetTokenSource(staticTokenSource{tok: "tok-123"})

	if _, err := a.Complete(context.Background(), "hi"); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if gotAuth != "Bearer tok-123" {
		t.Errorf("Authorization = %q, want Bearer tok-123", gotAuth)
	}
	if gotAPIKey != "" {
		t.Errorf("x-api-key set in oauth mode: %q", gotAPIKey)
	}
	if gotBeta != "claude-code-20250219,oauth-2025-04-20" {
		t.Errorf("anthropic-beta = %q", gotBeta)
	}
	if gotUA == "" {
		t.Errorf("missing user-agent")
	}
	if gotXApp != "cli" {
		t.Errorf("x-app = %q", gotXApp)
	}
}
