// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package anthropicauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestClientID(t *testing.T) {
	id := ClientID()
	if id != "9d1c250a-e61b-44d9-88ed-5944d1962f5e" {
		t.Errorf("ClientID() = %q", id)
	}
}

func TestAuthorizeURLWithParams(t *testing.T) {
	u := AuthorizeURLWithParams("chal", "state-v")
	for _, want := range []string{
		"https://claude.ai/oauth/authorize?",
		"client_id=9d1c250a",
		"code_challenge=chal",
		"code_challenge_method=S256",
		"state=state-v",
		"redirect_uri=http%3A%2F%2Flocalhost%3A53692%2Fcallback",
	} {
		if !strings.Contains(u, want) {
			t.Errorf("authorize URL missing %q:\n%s", want, u)
		}
	}
}

func tokenServer(t *testing.T, wantGrant string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["grant_type"] != wantGrant {
			t.Errorf("grant_type = %q, want %q", body["grant_type"], wantGrant)
		}
		if body["client_id"] != ClientID() {
			t.Errorf("client_id = %q", body["client_id"])
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "AT-" + wantGrant,
			"refresh_token": "RT-" + wantGrant,
			"expires_in":    3600,
		})
	}))
}

func TestRefresh(t *testing.T) {
	srv := tokenServer(t, "refresh_token")
	defer srv.Close()

	creds, err := Refresh(context.Background(), srv.Client(), srv.URL, "old-refresh")
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if creds.Access != "AT-refresh_token" || creds.Refresh != "RT-refresh_token" {
		t.Errorf("got creds %+v", creds)
	}
	if creds.ExpiresMs <= time.Now().UnixMilli() {
		t.Errorf("ExpiresMs should be in the future, got %d (now %d)", creds.ExpiresMs, time.Now().UnixMilli())
	}
	// Should be roughly now + 3600s - 5min skew
	wantApprox := time.Now().Add(55 * time.Minute).UnixMilli()
	if diff := creds.ExpiresMs - wantApprox; diff < -5_000 || diff > 5_000 {
		t.Errorf("expiry off by %dms", diff)
	}
}

func TestTokenSource_returnsCachedToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.json")
	_ = Save(path, &Credentials{
		Access:    "still-good",
		Refresh:   "r",
		ExpiresMs: time.Now().Add(1 * time.Hour).UnixMilli(),
	})
	ts := NewTokenSource(path)
	got, err := ts.AccessToken(context.Background())
	if err != nil {
		t.Fatalf("AccessToken: %v", err)
	}
	if got != "still-good" {
		t.Errorf("got %q", got)
	}
}

func TestTokenSource_refreshesExpired(t *testing.T) {
	srv := tokenServer(t, "refresh_token")
	defer srv.Close()

	path := filepath.Join(t.TempDir(), "c.json")
	_ = Save(path, &Credentials{
		Access:    "old",
		Refresh:   "r",
		ExpiresMs: time.Now().Add(-1 * time.Hour).UnixMilli(),
	})
	ts := &TokenSource{path: path, httpClient: srv.Client(), tokenURLOver: srv.URL}

	got, err := ts.AccessToken(context.Background())
	if err != nil {
		t.Fatalf("AccessToken: %v", err)
	}
	if got != "AT-refresh_token" {
		t.Errorf("got %q, want refreshed token", got)
	}
	// Persisted to disk
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.Access != "AT-refresh_token" {
		t.Errorf("disk creds not updated: %+v", c)
	}
}

func TestTokenSource_noFile(t *testing.T) {
	ts := NewTokenSource(filepath.Join(t.TempDir(), "missing.json"))
	if _, err := ts.AccessToken(context.Background()); err == nil {
		t.Error("expected error from missing credentials file")
	}
}
