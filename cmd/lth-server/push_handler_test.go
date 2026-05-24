// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mattdurham/lth/internal/blobstore"
	"github.com/mattdurham/lth/internal/parquet"
	"github.com/mattdurham/lth/internal/wire"
)

func makeTestStore(t *testing.T) *blobstore.LocalStore {
	t.Helper()
	s, err := blobstore.NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocalStore: %v", err)
	}
	return s
}

func makeZIP(t *testing.T, memories []wire.ExportMemory) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	var files []string
	if len(memories) > 0 {
		layer := memories[0].Layer
		fname := "memories_l" + itoa(layer) + "_000.jsonl"
		w, err := zw.Create(fname)
		if err != nil {
			t.Fatal(err)
		}
		for i := range memories {
			b, _ := json.Marshal(&memories[i])
			b = append(b, '\n')
			w.Write(b)
		}
		files = append(files, fname)
	}

	manifest := wire.ExportManifest{
		ExportedAt:  time.Now().UTC(),
		ChunkSize:   1000,
		MemoryCount: len(memories),
		Files:       files,
	}
	mw, _ := zw.Create("manifest.json")
	b, _ := json.MarshalIndent(manifest, "", "  ")
	mw.Write(b)
	zw.Close()
	return buf.Bytes()
}

func itoa(n int) string {
	return strings.TrimSpace(strings.Replace("12345"[:n], "", "", -1))
}

func makeMemory(layer int, source string) wire.ExportMemory {
	return wire.ExportMemory{
		ID:          "mem-" + strings.Repeat("a", 8),
		Layer:       layer,
		Content:     "test content",
		ContentHash: "hash123",
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
		Source:      source,
	}
}

func newPushHandler(t *testing.T, store *blobstore.LocalStore) *PushHandler {
	return &PushHandler{
		store:  store,
		writer: parquet.NewWriter(),
		cfg:    defaultServerConfig(),
	}
}

func pushHeaders(account, org, user, team string) http.Header {
	h := http.Header{}
	h.Set("X-LTH-Account", account)
	h.Set("X-LTH-Org", org)
	h.Set("X-LTH-User", user)
	if team != "" {
		h.Set("X-LTH-Team", team)
	}
	return h
}

func TestPushHandler_EmptyZIP(t *testing.T) {
	store := makeTestStore(t)
	handler := newPushHandler(t, store)

	body := makeZIP(t, nil)
	req := httptest.NewRequest(http.MethodPost, "/v1/sync/push", bytes.NewReader(body))
	req.Header = pushHeaders("acct", "org", "user", "")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	var resp pushResponse
	json.NewDecoder(rr.Body).Decode(&resp)
	if resp.Accepted != 0 || resp.Skipped != 0 {
		t.Errorf("got accepted=%d skipped=%d want 0,0", resp.Accepted, resp.Skipped)
	}
}

func TestPushHandler_SingleMemory(t *testing.T) {
	store := makeTestStore(t)
	handler := newPushHandler(t, store)

	body := makeZIP(t, []wire.ExportMemory{makeMemory(1, "local")})
	req := httptest.NewRequest(http.MethodPost, "/v1/sync/push", bytes.NewReader(body))
	req.Header = pushHeaders("acct", "org", "user", "")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	var resp pushResponse
	json.NewDecoder(rr.Body).Decode(&resp)
	if resp.Accepted != 1 {
		t.Errorf("got accepted=%d want 1", resp.Accepted)
	}

	objs, _ := store.List(t.Context(), "")
	if len(objs) == 0 {
		t.Error("expected parquet file in store")
	}
}

func TestPushHandler_SkipsServerSource(t *testing.T) {
	store := makeTestStore(t)
	handler := newPushHandler(t, store)

	mem := makeMemory(1, "server")
	body := makeZIP(t, []wire.ExportMemory{mem})
	req := httptest.NewRequest(http.MethodPost, "/v1/sync/push", bytes.NewReader(body))
	req.Header = pushHeaders("acct", "org", "user", "")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status %d", rr.Code)
	}
	var resp pushResponse
	json.NewDecoder(rr.Body).Decode(&resp)
	if resp.Accepted != 0 || resp.Skipped != 1 {
		t.Errorf("got accepted=%d skipped=%d want 0,1", resp.Accepted, resp.Skipped)
	}
}

func TestPushHandler_MissingHeaders(t *testing.T) {
	store := makeTestStore(t)
	handler := newPushHandler(t, store)

	body := makeZIP(t, nil)
	req := httptest.NewRequest(http.MethodPost, "/v1/sync/push", bytes.NewReader(body))
	// No identity headers.
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("got %d want 400", rr.Code)
	}
}

func TestPushHandler_L3ScopeShared(t *testing.T) {
	store := makeTestStore(t)
	handler := newPushHandler(t, store)

	body := makeZIP(t, []wire.ExportMemory{makeMemory(3, "local")})
	req := httptest.NewRequest(http.MethodPost, "/v1/sync/push", bytes.NewReader(body))
	req.Header = pushHeaders("acct", "org", "user", "")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status %d", rr.Code)
	}
	objs, _ := store.List(t.Context(), "acct/org/shared/L3/")
	if len(objs) == 0 {
		t.Error("expected parquet in shared/L3/ prefix")
	}
}

func TestPushHandler_L4WithTeam(t *testing.T) {
	store := makeTestStore(t)
	handler := newPushHandler(t, store)

	body := makeZIP(t, []wire.ExportMemory{makeMemory(4, "local")})
	req := httptest.NewRequest(http.MethodPost, "/v1/sync/push", bytes.NewReader(body))
	req.Header = pushHeaders("acct", "org", "user", "myteam")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status %d", rr.Code)
	}
	objs, _ := store.List(t.Context(), "acct/org/teams/myteam/L4/")
	if len(objs) == 0 {
		t.Error("expected parquet in teams/myteam/L4/ prefix")
	}
}

func TestPushHandler_L4NoTeam(t *testing.T) {
	store := makeTestStore(t)
	handler := newPushHandler(t, store)

	body := makeZIP(t, []wire.ExportMemory{makeMemory(4, "local")})
	req := httptest.NewRequest(http.MethodPost, "/v1/sync/push", bytes.NewReader(body))
	req.Header = pushHeaders("acct", "org", "user", "")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status %d", rr.Code)
	}
	objs, _ := store.List(t.Context(), "acct/org/shared/L4/")
	if len(objs) == 0 {
		t.Error("expected parquet in shared/L4/ prefix")
	}
}

func TestPushHandler_ParquetDisabled(t *testing.T) {
	store := makeTestStore(t)
	cfg := defaultServerConfig()
	cfg.Parquet.Enabled = false
	handler := &PushHandler{store: store, writer: parquet.NewWriter(), cfg: cfg}

	body := makeZIP(t, []wire.ExportMemory{makeMemory(1, "local")})
	req := httptest.NewRequest(http.MethodPost, "/v1/sync/push", bytes.NewReader(body))
	req.Header = pushHeaders("acct", "org", "user", "")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status %d", rr.Code)
	}
	objs, _ := store.List(t.Context(), "")
	if len(objs) != 0 {
		t.Error("expected no parquet files when Parquet.Enabled=false")
	}
}

func TestPushHandler_InvalidZIP(t *testing.T) {
	store := makeTestStore(t)
	handler := newPushHandler(t, store)

	req := httptest.NewRequest(http.MethodPost, "/v1/sync/push", strings.NewReader("not a zip"))
	req.Header = pushHeaders("acct", "org", "user", "")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("got %d want 400", rr.Code)
	}
}

// Ensure unused io import is not an issue.
var _ = io.Discard