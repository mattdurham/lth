// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package main

import (
	"archive/zip"
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mattdurham/lth/internal/wire"
)

// makeMultiLayerZIP builds a ZIP with memories across multiple layers.
func makeMultiLayerZIP(t *testing.T, byLayer map[int][]wire.ExportMemory) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	var files []string
	total := 0
	for layer, mems := range byLayer {
		fname := "memories_l" + itoa(layer) + "_000.jsonl"
		w, err := zw.Create(fname)
		if err != nil {
			t.Fatal(err)
		}
		for i := range mems {
			b, _ := json.Marshal(&mems[i])
			b = append(b, '\n')
			w.Write(b) //nolint:errcheck
		}
		files = append(files, fname)
		total += len(mems)
	}
	manifest := wire.ExportManifest{
		ExportedAt:  time.Now().UTC(),
		ChunkSize:   1000,
		MemoryCount: total,
		Files:       files,
	}
	mw, _ := zw.Create("manifest.json")
	b, _ := json.MarshalIndent(manifest, "", "  ")
	mw.Write(b) //nolint:errcheck
	zw.Close()  //nolint:errcheck
	return buf.Bytes()
}

// decodeZIPMemories unpacks a ZIP response and returns all memories.
func decodeZIPMemories(t *testing.T, body []byte) []wire.ExportMemory {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	var all []wire.ExportMemory
	for _, f := range zr.File {
		if len(f.Name) < 9 || f.Name[:9] != "memories_" {
			continue
		}
		rc, _ := f.Open()
		sc := bufio.NewScanner(rc)
		for sc.Scan() {
			var m wire.ExportMemory
			if err := json.Unmarshal(sc.Bytes(), &m); err == nil {
				all = append(all, m)
			}
		}
		rc.Close() //nolint:errcheck
	}
	return all
}

// TestPushPull_Roundtrip pushes memories and verifies pull returns them.
func TestPushPull_Roundtrip(t *testing.T) {
	store := makeTestStore(t)
	push := newPushHandler(t, store)
	pull := newPullHandler(t, store)

	mems := []wire.ExportMemory{
		{ID: "m1", Layer: 3, Content: "Go concurrency technique", ContentHash: "hash-m1",
			CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()},
		{ID: "m2", Layer: 4, Content: "Project context note", ContentHash: "hash-m2",
			CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()},
	}

	// Push L3 and L4 memories.
	zipBody := makeMultiLayerZIP(t, map[int][]wire.ExportMemory{
		3: {mems[0]},
		4: {mems[1]},
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/sync/push", bytes.NewReader(zipBody))
	req.Header = pushHeaders("acme", "eng", "alice", "backend")
	w := httptest.NewRecorder()
	push.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("push status = %d, want 200; body: %s", w.Code, w.Body.String())
	}

	// Pull everything since epoch.
	req2 := httptest.NewRequest(http.MethodGet, "/v1/sync/pull?since=2000-01-01T00:00:00Z&layers=3,4", nil)
	req2.Header = pushHeaders("acme", "eng", "alice", "backend")
	w2 := httptest.NewRecorder()
	pull.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("pull status = %d, want 200; body: %s", w2.Code, w2.Body.String())
	}

	body, _ := io.ReadAll(w2.Body)
	got := decodeZIPMemories(t, body)
	if len(got) != 2 {
		t.Errorf("pull returned %d memories, want 2", len(got))
	}
	for _, m := range got {
		if m.Source != "server" {
			t.Errorf("memory %s has source=%q, want server", m.ID, m.Source)
		}
	}
}

// TestPushPull_Dedup pushes the same content twice; second push should be skipped.
func TestPushPull_Dedup(t *testing.T) {
	store := makeTestStore(t)
	push := newPushHandler(t, store)
	pull := newPullHandler(t, store)

	mem := wire.ExportMemory{
		ID: "m1", Layer: 3, Content: "identical content", ContentHash: "same-hash",
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}

	doPush := func() map[string]int {
		zipBody := makeZIP(t, []wire.ExportMemory{mem})
		req := httptest.NewRequest(http.MethodPost, "/v1/sync/push", bytes.NewReader(zipBody))
		req.Header = pushHeaders("acme", "eng", "alice", "")
		w := httptest.NewRecorder()
		push.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("push status = %d: %s", w.Code, w.Body.String())
		}
		var result map[string]int
		json.Unmarshal(w.Body.Bytes(), &result) //nolint:errcheck
		return result
	}

	// First push: accepted=1, skipped=0.
	r1 := doPush()
	if r1["accepted"] != 1 || r1["skipped"] != 0 {
		t.Errorf("first push: accepted=%d skipped=%d, want 1/0", r1["accepted"], r1["skipped"])
	}

	// Second push of identical content: accepted=0, skipped=1.
	r2 := doPush()
	if r2["accepted"] != 0 || r2["skipped"] != 1 {
		t.Errorf("second push: accepted=%d skipped=%d, want 0/1", r2["accepted"], r2["skipped"])
	}

	// Pull should return exactly one memory (not two).
	req := httptest.NewRequest(http.MethodGet, "/v1/sync/pull?since=2000-01-01T00:00:00Z&layers=3", nil)
	req.Header = pushHeaders("acme", "eng", "alice", "")
	w := httptest.NewRecorder()
	pull.ServeHTTP(w, req)
	body, _ := io.ReadAll(w.Body)
	got := decodeZIPMemories(t, body)
	if len(got) != 1 {
		t.Errorf("pull after dedup: got %d memories, want 1", len(got))
	}
}

// TestPushPull_AllLayers pushes all four layers and verifies visibility scoping.
func TestPushPull_AllLayers(t *testing.T) {
	store := makeTestStore(t)
	push := newPushHandler(t, store)
	pull := newPullHandler(t, store)

	byLayer := map[int][]wire.ExportMemory{
		1: {{ID: "l1", Layer: 1, Content: "L1 principle", ContentHash: "h1", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}},
		2: {{ID: "l2", Layer: 2, Content: "L2 rule", ContentHash: "h2", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}},
		3: {{ID: "l3", Layer: 3, Content: "L3 technique", ContentHash: "h3", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}},
		4: {{ID: "l4", Layer: 4, Content: "L4 context", ContentHash: "h4", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}},
	}

	zipBody := makeMultiLayerZIP(t, byLayer)
	req := httptest.NewRequest(http.MethodPost, "/v1/sync/push", bytes.NewReader(zipBody))
	req.Header = pushHeaders("acme", "eng", "alice", "backend")
	w := httptest.NewRecorder()
	push.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("push: %d %s", w.Code, w.Body)
	}

	// Pull all 4 layers.
	req2 := httptest.NewRequest(http.MethodGet, "/v1/sync/pull?since=2000-01-01T00:00:00Z&layers=1,2,3,4", nil)
	req2.Header = pushHeaders("acme", "eng", "alice", "backend")
	w2 := httptest.NewRecorder()
	pull.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("pull: %d %s", w2.Code, w2.Body)
	}
	body, _ := io.ReadAll(w2.Body)
	got := decodeZIPMemories(t, body)
	if len(got) != 4 {
		t.Errorf("pull all layers: got %d memories, want 4", len(got))
	}
}

// TestObserveHandler_Write verifies L5 observations are written to the store.
func TestObserveHandler_Write(t *testing.T) {
	store := makeTestStore(t)
	handler := &ObserveHandler{store: store, writer: newPushHandler(t, store).writer}

	obs := wire.ExportMemory{
		ID: "obs1", Layer: 5, Content: "raw observation", ContentHash: "obs-hash",
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	b, _ := json.Marshal(obs)

	req := httptest.NewRequest(http.MethodPost, "/v1/observations", bytes.NewReader(append(b, '\n')))
	req.Header = pushHeaders("acme", "eng", "alice", "")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Errorf("observe status = %d, want 204; body: %s", w.Code, w.Body.String())
	}

	// Verify something was written to the store under L5 prefix.
	keys, err := store.List(t.Context(), "acme/eng/L5/")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(keys) == 0 {
		t.Error("observe wrote nothing to store")
	}
}
