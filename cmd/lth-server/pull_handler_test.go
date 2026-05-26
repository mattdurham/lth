// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package main

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mattdurham/lth/internal/blobstore"
	"github.com/mattdurham/lth/internal/parquet"
	"github.com/mattdurham/lth/internal/wire"
)

func newPullHandler(t *testing.T, store *blobstore.LocalStore) *PullHandler {
	t.Helper()
	return &PullHandler{
		store:  store,
		reader: parquet.NewReader(),
	}
}

func storeParquet(t *testing.T, store blobstore.BlobStore, key string, records []parquet.MemoryRecord) {
	t.Helper()
	ctx := context.Background()
	w := parquet.NewWriter()
	var buf bytes.Buffer
	if err := w.Write(ctx, &buf, records); err != nil {
		t.Fatalf("write parquet: %v", err)
	}
	if err := store.Put(ctx, key, &buf); err != nil {
		t.Fatalf("put store: %v", err)
	}
}

func pullMemories(t *testing.T, body []byte) []wire.ExportMemory {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	var memories []wire.ExportMemory
	for _, f := range zr.File {
		if len(f.Name) < 10 || f.Name[:9] != "memories_" {
			continue
		}
		rc, _ := f.Open()
		scanner := bufio.NewScanner(rc)
		for scanner.Scan() {
			var m wire.ExportMemory
			json.Unmarshal(scanner.Bytes(), &m)
			memories = append(memories, m)
		}
		rc.Close()
	}
	return memories
}

func TestPullHandler_EmptyStore(t *testing.T) {
	store := makeTestStore(t)
	handler := newPullHandler(t, store)

	req := httptest.NewRequest(http.MethodGet, "/v1/sync/pull", nil)
	req.Header = pushHeaders("acct", "org", "user", "")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	mems := pullMemories(t, rr.Body.Bytes())
	if len(mems) != 0 {
		t.Errorf("got %d memories want 0", len(mems))
	}
}

func TestPullHandler_MissingHeaders(t *testing.T) {
	store := makeTestStore(t)
	handler := newPullHandler(t, store)

	req := httptest.NewRequest(http.MethodGet, "/v1/sync/pull", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("got %d want 400", rr.Code)
	}
}

func TestPullHandler_L5Excluded(t *testing.T) {
	store := makeTestStore(t)
	handler := newPullHandler(t, store)

	req := httptest.NewRequest(http.MethodGet, "/v1/sync/pull?layers=5", nil)
	req.Header = pushHeaders("acct", "org", "user", "")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("got %d want 400 (L5 has no pull endpoint)", rr.Code)
	}
}

func TestPullHandler_SourceSetToServer(t *testing.T) {
	store := makeTestStore(t)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	rec := parquet.MemoryRecord{
		ID: "r1", Layer: 1, Content: "hi", ContentHash: "ch1",
		CreatedAt: base, UpdatedAt: base, Source: "local",
	}
	storeParquet(t, store, "acct/org/users/user/L1/date=2026-01-01/p.parquet", []parquet.MemoryRecord{rec})

	handler := newPullHandler(t, store)
	req := httptest.NewRequest(http.MethodGet, "/v1/sync/pull?layers=1", nil)
	req.Header = pushHeaders("acct", "org", "user", "")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	mems := pullMemories(t, rr.Body.Bytes())
	if len(mems) != 1 {
		t.Fatalf("got %d memories want 1", len(mems))
	}
	if mems[0].Source != "server" {
		t.Errorf("source=%q want server", mems[0].Source)
	}
}

func TestPullHandler_SinceFilter(t *testing.T) {
	store := makeTestStore(t)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	recs := []parquet.MemoryRecord{
		{ID: "r1", Layer: 1, Content: "old", ContentHash: "ch1", CreatedAt: base, UpdatedAt: base},
		{ID: "r2", Layer: 1, Content: "new", ContentHash: "ch2", CreatedAt: base.Add(2 * time.Hour), UpdatedAt: base.Add(2 * time.Hour)},
	}
	storeParquet(t, store, "acct/org/users/user/L1/date=2026-01-01/p.parquet", recs)

	handler := newPullHandler(t, store)
	since := base.Add(time.Hour).Format(time.RFC3339)
	req := httptest.NewRequest(http.MethodGet, "/v1/sync/pull?layers=1&since="+since, nil)
	req.Header = pushHeaders("acct", "org", "user", "")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status %d", rr.Code)
	}
	mems := pullMemories(t, rr.Body.Bytes())
	if len(mems) != 1 {
		t.Fatalf("got %d memories want 1", len(mems))
	}
	if mems[0].ID != "r2" {
		t.Errorf("got ID=%s want r2", mems[0].ID)
	}
}

func TestPullHandler_LayersParam(t *testing.T) {
	store := makeTestStore(t)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	l1 := parquet.MemoryRecord{ID: "l1", Layer: 1, ContentHash: "h1", CreatedAt: base, UpdatedAt: base}
	l3 := parquet.MemoryRecord{ID: "l3", Layer: 3, ContentHash: "h3", CreatedAt: base, UpdatedAt: base}
	storeParquet(t, store, "acct/org/users/user/L1/date=2026-01-01/p.parquet", []parquet.MemoryRecord{l1})
	storeParquet(t, store, "acct/org/shared/L3/date=2026-01-01/p.parquet", []parquet.MemoryRecord{l3})

	handler := newPullHandler(t, store)
	req := httptest.NewRequest(http.MethodGet, "/v1/sync/pull?layers=1", nil)
	req.Header = pushHeaders("acct", "org", "user", "")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status %d", rr.Code)
	}
	mems := pullMemories(t, rr.Body.Bytes())
	if len(mems) != 1 {
		t.Fatalf("got %d memories want 1 (only L1)", len(mems))
	}
	if mems[0].ID != "l1" {
		t.Errorf("got ID=%s want l1", mems[0].ID)
	}
}
