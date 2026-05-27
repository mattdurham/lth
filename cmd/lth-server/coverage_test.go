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

	"github.com/mattdurham/lth/internal/parquet"
	"github.com/mattdurham/lth/internal/wire"
)

// --- ObserveHandler error paths ---

func TestObserveHandler_WrongMethod(t *testing.T) {
	store := makeTestStore(t)
	h := &ObserveHandler{store: store, writer: newPushHandler(t, store).writer}
	req := httptest.NewRequest(http.MethodGet, "/v1/observations", nil)
	req.Header = pushHeaders("acme", "eng", "alice", "")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("wrong method: got %d want 405", w.Code)
	}
}

func TestObserveHandler_MissingHeaders(t *testing.T) {
	store := makeTestStore(t)
	h := &ObserveHandler{store: store, writer: newPushHandler(t, store).writer}
	req := httptest.NewRequest(http.MethodPost, "/v1/observations", strings.NewReader("{}"))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("missing headers: got %d want 400", w.Code)
	}
}

func TestObserveHandler_EmptyBody(t *testing.T) {
	store := makeTestStore(t)
	h := &ObserveHandler{store: store, writer: newPushHandler(t, store).writer}
	req := httptest.NewRequest(http.MethodPost, "/v1/observations", strings.NewReader(""))
	req.Header = pushHeaders("acme", "eng", "alice", "")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Errorf("empty body: got %d want 204", w.Code)
	}
}

func TestObserveHandler_MalformedNDJSON(t *testing.T) {
	store := makeTestStore(t)
	h := &ObserveHandler{store: store, writer: newPushHandler(t, store).writer}
	req := httptest.NewRequest(http.MethodPost, "/v1/observations", strings.NewReader("not json\n"))
	req.Header = pushHeaders("acme", "eng", "alice", "")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	// Malformed lines are skipped; valid ones still written. 204 expected.
	if w.Code != http.StatusNoContent && w.Code != http.StatusBadRequest {
		t.Errorf("malformed ndjson: got %d", w.Code)
	}
}

// --- PullHandler error paths ---

func TestPullHandler_WrongMethod(t *testing.T) {
	store := makeTestStore(t)
	h := newPullHandler(t, store)
	req := httptest.NewRequest(http.MethodPost, "/v1/sync/pull", nil)
	req.Header = pushHeaders("acme", "eng", "alice", "")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("wrong method: got %d want 405", w.Code)
	}
}

func TestPullHandler_BadSince(t *testing.T) {
	store := makeTestStore(t)
	h := newPullHandler(t, store)
	req := httptest.NewRequest(http.MethodGet, "/v1/sync/pull?since=not-a-time&layers=3", nil)
	req.Header = pushHeaders("acme", "eng", "alice", "")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("bad since: got %d want 400", w.Code)
	}
}

func TestPullHandler_InvalidLayer(t *testing.T) {
	store := makeTestStore(t)
	h := newPullHandler(t, store)
	req := httptest.NewRequest(http.MethodGet, "/v1/sync/pull?since=2000-01-01T00:00:00Z&layers=99", nil)
	req.Header = pushHeaders("acme", "eng", "alice", "")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("invalid layer: got %d want 400", w.Code)
	}
}

func TestPullHandler_NoLayers(t *testing.T) {
	store := makeTestStore(t)
	h := newPullHandler(t, store)
	req := httptest.NewRequest(http.MethodGet, "/v1/sync/pull?since=2000-01-01T00:00:00Z", nil)
	req.Header = pushHeaders("acme", "eng", "alice", "")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	// No layers = no data = empty zip, still 200.
	if w.Code != http.StatusOK {
		t.Errorf("no layers: got %d want 200", w.Code)
	}
}

// --- PushHandler error paths ---

func TestPushHandler_WrongMethod(t *testing.T) {
	store := makeTestStore(t)
	h := newPushHandler(t, store)
	req := httptest.NewRequest(http.MethodGet, "/v1/sync/push", nil)
	req.Header = pushHeaders("acme", "eng", "alice", "")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("wrong method: got %d want 405", w.Code)
	}
}

func TestPushHandler_MissingManifest(t *testing.T) {
	// ZIP with a memories file but no manifest.json
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	f, _ := zw.Create("memories_l3_000.jsonl")
	f.Write([]byte{}) //nolint:errcheck
	zw.Close()        //nolint:errcheck

	store := makeTestStore(t)
	h := newPushHandler(t, store)
	req := httptest.NewRequest(http.MethodPost, "/v1/sync/push", bytes.NewReader(buf.Bytes()))
	req.Header = pushHeaders("acme", "eng", "alice", "")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("missing manifest: got %d want 400", w.Code)
	}
}

func TestPushHandler_ManifestReferencesAbsentFile(t *testing.T) {
	// manifest.json lists a file that doesn't exist in the ZIP.
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	manifest := wire.ExportManifest{
		Files:       []string{"memories_l3_000.jsonl"}, // not in zip
		MemoryCount: 1,
	}
	mw, _ := zw.Create("manifest.json")
	b, _ := json.MarshalIndent(manifest, "", "  ")
	mw.Write(b) //nolint:errcheck
	zw.Close()  //nolint:errcheck

	store := makeTestStore(t)
	h := newPushHandler(t, store)
	req := httptest.NewRequest(http.MethodPost, "/v1/sync/push", bytes.NewReader(buf.Bytes()))
	req.Header = pushHeaders("acme", "eng", "alice", "")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("absent file: got %d want 400", w.Code)
	}
}

func TestPushHandler_MultipleChunks(t *testing.T) {
	// Two memory chunks for L3, verify both are accepted.
	mem1 := wire.ExportMemory{ID: "a", Layer: 3, Content: "first", ContentHash: "h-a",
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	mem2 := wire.ExportMemory{ID: "b", Layer: 3, Content: "second", ContentHash: "h-b",
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	writeJSONL := func(name string, mems []wire.ExportMemory) {
		f, _ := zw.Create(name)
		for _, m := range mems {
			b, _ := json.Marshal(m)
			f.Write(append(b, '\n')) //nolint:errcheck
		}
	}
	writeJSONL("memories_l3_000.jsonl", []wire.ExportMemory{mem1})
	writeJSONL("memories_l3_001.jsonl", []wire.ExportMemory{mem2})
	manifest := wire.ExportManifest{
		Files:       []string{"memories_l3_000.jsonl", "memories_l3_001.jsonl"},
		MemoryCount: 2,
	}
	mw, _ := zw.Create("manifest.json")
	b, _ := json.MarshalIndent(manifest, "", "  ")
	mw.Write(b) //nolint:errcheck
	zw.Close()  //nolint:errcheck

	store := makeTestStore(t)
	h := newPushHandler(t, store)
	req := httptest.NewRequest(http.MethodPost, "/v1/sync/push", bytes.NewReader(buf.Bytes()))
	req.Header = pushHeaders("acme", "eng", "alice", "")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("multi-chunk: got %d: %s", w.Code, w.Body)
	}
	var result map[string]int
	json.Unmarshal(w.Body.Bytes(), &result) //nolint:errcheck
	if result["accepted"] != 2 {
		t.Errorf("multi-chunk: accepted=%d want 2", result["accepted"])
	}
}

// --- parquet layer helpers ---

func TestLayerScope_L1(t *testing.T) {
	if s := layerScope(1, "alice", ""); s != "users/alice" {
		t.Errorf("L1 scope = %q want users/alice", s)
	}
}

func TestLayerScope_L2(t *testing.T) {
	if s := layerScope(2, "alice", "backend"); s != "users/alice" {
		t.Errorf("L2 scope = %q want users/alice", s)
	}
}

func TestLayerScope_L3(t *testing.T) {
	if s := layerScope(3, "alice", "backend"); s != "shared" {
		t.Errorf("L3 scope = %q want shared", s)
	}
}

func TestLayerScope_L4WithTeam(t *testing.T) {
	if s := layerScope(4, "alice", "backend"); s != "teams/backend" {
		t.Errorf("L4+team scope = %q want teams/backend", s)
	}
}

func TestLayerScope_L4NoTeam(t *testing.T) {
	if s := layerScope(4, "alice", ""); s != "shared" {
		t.Errorf("L4 no team scope = %q want shared", s)
	}
}

// --- parseSince edge cases ---

func TestParseSince_RFC3339(t *testing.T) {
	ts, err := parseSince("2026-01-15T10:30:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if ts.Year() != 2026 {
		t.Errorf("year = %d want 2026", ts.Year())
	}
}

func TestParseSince_InvalidFormat(t *testing.T) {
	_, err := parseSince("not-a-date")
	if err == nil {
		t.Error("expected error for invalid date format")
	}
}

func TestParseSince_Empty(t *testing.T) {
	ts, err := parseSince("")
	if err != nil {
		t.Fatal(err)
	}
	if !ts.IsZero() {
		t.Error("empty since should return zero time")
	}
}

// --- keyDateGe ---

func TestKeyDateGe_Match(t *testing.T) {
	if !keyDateGe("acme/eng/shared/L3/date=2026-05-15/push.parquet", "2026-05-01") {
		t.Error("expected true for date after since")
	}
}

func TestKeyDateGe_Before(t *testing.T) {
	if keyDateGe("acme/eng/shared/L3/date=2026-05-15/push.parquet", "2026-05-20") {
		t.Error("expected false for date before since")
	}
}

func TestKeyDateGe_NoDate(t *testing.T) {
	// Keys without date= segment pass through (true) — they're not date-filtered.
	if !keyDateGe("acme/eng/shared/L3/push.parquet", "2026-05-01") {
		t.Error("expected true for key with no date segment (pass-through)")
	}
}

// --- Parquet round-trip via push+pull with edges ---

func TestPushPull_WithEdges(t *testing.T) {
	store := makeTestStore(t)
	push := newPushHandler(t, store)
	pull := newPullHandler(t, store)

	mem := wire.ExportMemory{ID: "m1", Layer: 3, Content: "technique", ContentHash: "hx",
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	edge := wire.ExportEdge{ID: "e1", FromID: "m1", ToID: "m2", EdgeType: "relates_to", Weight: 1.0}

	// Build ZIP with both memory and edge files.
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	mf, _ := zw.Create("memories_l3_000.jsonl")
	mb, _ := json.Marshal(mem)
	mf.Write(append(mb, '\n')) //nolint:errcheck
	ef, _ := zw.Create("edges_000.jsonl")
	eb, _ := json.Marshal(edge)
	ef.Write(append(eb, '\n')) //nolint:errcheck
	manifest := wire.ExportManifest{Files: []string{"memories_l3_000.jsonl", "edges_000.jsonl"}, MemoryCount: 1}
	mfw, _ := zw.Create("manifest.json")
	b, _ := json.MarshalIndent(manifest, "", "  ")
	mfw.Write(b) //nolint:errcheck
	zw.Close()   //nolint:errcheck

	req := httptest.NewRequest(http.MethodPost, "/v1/sync/push", bytes.NewReader(buf.Bytes()))
	req.Header = pushHeaders("acme", "eng", "alice", "")
	w := httptest.NewRecorder()
	push.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("push: %d %s", w.Code, w.Body)
	}

	// Pull and verify memory returned.
	req2 := httptest.NewRequest(http.MethodGet, "/v1/sync/pull?since=2000-01-01T00:00:00Z&layers=3", nil)
	req2.Header = pushHeaders("acme", "eng", "alice", "")
	w2 := httptest.NewRecorder()
	pull.ServeHTTP(w2, req2)
	body, _ := io.ReadAll(w2.Body)
	got := decodeZIPMemories(t, body)
	if len(got) != 1 {
		t.Errorf("got %d memories want 1", len(got))
	}
}

// Verify parquet.NewReader and NewWriter are exercised via the handlers.
func TestParquetConfig_Disabled(t *testing.T) {
	store := makeTestStore(t)
	cfg := defaultServerConfig()
	cfg.Parquet.Enabled = false
	h := &PushHandler{store: store, writer: parquet.NewWriter(), cfg: cfg}

	zipBody := makeZIP(t, []wire.ExportMemory{
		{ID: "x", Layer: 3, Content: "c", ContentHash: "ch", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()},
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/sync/push", bytes.NewReader(zipBody))
	req.Header = pushHeaders("acme", "eng", "alice", "")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("parquet disabled: %d %s", w.Code, w.Body)
	}
	var result map[string]int
	json.Unmarshal(w.Body.Bytes(), &result) //nolint:errcheck
	if result["accepted"] != 1 {
		t.Errorf("accepted = %d want 1", result["accepted"])
	}
}
