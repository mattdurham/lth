// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mattdurham/lth/internal/blobstore"
	"github.com/mattdurham/lth/internal/parquet"
	"github.com/mattdurham/lth/internal/vector"
	"github.com/mattdurham/lth/internal/wire"
)

// PullHandler handles GET /v1/sync/pull.

func (h *PullHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	account := r.Header.Get("X-LTH-Account")
	org := r.Header.Get("X-LTH-Org")
	user := r.Header.Get("X-LTH-User")
	team := r.Header.Get("X-LTH-Team")
	if account == "" || org == "" || user == "" {
		http.Error(w, "X-LTH-Account, X-LTH-Org, X-LTH-User headers required", http.StatusBadRequest)
		return
	}

	since, err := parseSince(r.URL.Query().Get("since"))
	if err != nil {
		http.Error(w, "invalid since: "+err.Error(), http.StatusBadRequest)
		return
	}

	layers, err := parseLayers(r.URL.Query().Get("layers"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	var allRecords []parquet.MemoryRecord

	sinceDate := ""
	if !since.IsZero() {
		sinceDate = since.UTC().Format("2006-01-02")
	}

	for _, layer := range layers {
		scope := layerScope(layer, user, team)
		prefix := fmt.Sprintf("%s/%s/%s/L%d/", account, org, scope, layer)
		objs, listErr := h.store.List(ctx, prefix)
		if listErr != nil {
			http.Error(w, "list store: "+listErr.Error(), http.StatusInternalServerError)
			return
		}
		for _, obj := range objs {
			if sinceDate != "" && !keyDateGe(obj.Key, sinceDate) {
				continue
			}
			rc, getErr := h.store.Get(ctx, obj.Key)
			if getErr != nil {
				if errors.Is(getErr, fs.ErrNotExist) {
					continue
				}
				http.Error(w, "get object: "+getErr.Error(), http.StatusInternalServerError)
				return
			}
			recs, readErr := h.reader.Read(ctx, rc, since)
			rc.Close() //nolint:errcheck
			if readErr != nil {
				http.Error(w, "read parquet: "+readErr.Error(), http.StatusInternalServerError)
				return
			}
			allRecords = append(allRecords, recs...)
		}
	}

	// Overlay attrs sidecars — attrs are stored separately so they can be
	// updated without re-pushing content (which is immutable once indexed).
	overlayAttrs(ctx, h.store, account, org, allRecords)

	w.Header().Set("Content-Type", "application/zip")
	if err := buildZIPResponse(w, allRecords); err != nil {
		return
	}
}

// overlayAttrs fetches the attrs sidecar for each record and merges it over
// the record's embedded attrs. Sidecars win because they represent the most
// recent update; parquet attrs are from the original push.
func overlayAttrs(ctx context.Context, store blobstore.BlobStore, account, org string, records []parquet.MemoryRecord) {
	const concurrency = 20
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	for i := range records {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			rec := &records[i]
			attrsKey := fmt.Sprintf("%s/%s/attrs/%s/%s", account, org, rec.ContentHash[:2], rec.ContentHash)
			rc, err := store.Get(ctx, attrsKey)
			if err != nil {
				return // no sidecar, keep parquet attrs
			}
			defer rc.Close() //nolint:errcheck
			data, err := io.ReadAll(rc)
			if err != nil || len(data) == 0 {
				return
			}
			// Validate it's a JSON object before overwriting.
			if json.Valid(data) {
				rec.Attrs = string(data)
			}
		}(i)
	}
	wg.Wait()
}

// keyDateGe returns true if the BlobStore key contains a date= partition >= minDate.
// Key format: .../date=2026-01-15/file.parquet
func keyDateGe(key, minDate string) bool {
	const pfx = "date="
	idx := strings.Index(key, pfx)
	if idx < 0 {
		return true
	}
	datePart := key[idx+len(pfx):]
	if len(datePart) >= 10 {
		return datePart[:10] >= minDate
	}
	return true
}

func parseSince(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, err
	}
	return t, nil
}

func parseLayers(s string) ([]int, error) {
	if s == "" {
		return []int{1, 2, 3, 4, 5}, nil
	}
	parts := strings.Split(s, ",")
	layers := make([]int, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil, fmt.Errorf("invalid layer %q", p)
		}
		if n < 1 || n > 4 {
			// L5 is local-only (observations) and has no pull endpoint per SPEC item 5.
			return nil, fmt.Errorf("layer must be 1-4 (L5 is not pullable), got %d", n)
		}
		layers = append(layers, n)
	}
	if len(layers) == 0 {
		return []int{1, 2, 3, 4}, nil
	}
	return layers, nil
}

func buildZIPResponse(w io.Writer, records []parquet.MemoryRecord) error {
	const chunkSize = 1000
	now := time.Now().UTC()
	zw := zip.NewWriter(w)

	memories := make([]wire.ExportMemory, len(records))
	for i, r := range records {
		memories[i] = recordToExportMemory(r)
	}

	metadata := wire.ExportMetadata{
		LTHVersion:  "server",
		ExportedAt:  now,
		MemoryCount: len(memories),
	}
	if err := writeZIPEntry(zw, "metadata.json", metadata); err != nil {
		return err
	}

	var files []string
	byLayer := make(map[int][]wire.ExportMemory)
	for _, m := range memories {
		byLayer[m.Layer] = append(byLayer[m.Layer], m)
	}

	for _, layer := range []int{5, 4, 3, 2, 1} {
		rows := byLayer[layer]
		if len(rows) == 0 {
			continue
		}
		chunk := 0
		for start := 0; start < len(rows); start += chunkSize {
			end := start + chunkSize
			if end > len(rows) {
				end = len(rows)
			}
			filename := fmt.Sprintf("memories_l%d_%03d.jsonl", layer, chunk)
			if err := writeMemoryJSONL(zw, filename, rows[start:end]); err != nil {
				return err
			}
			files = append(files, filename)
			chunk++
		}
	}

	manifest := wire.ExportManifest{
		ExportedAt:  now,
		ChunkSize:   chunkSize,
		MemoryCount: len(memories),
		Files:       files,
	}
	if err := writeZIPEntry(zw, "manifest.json", manifest); err != nil {
		return err
	}

	return zw.Close()
}

func recordToExportMemory(r parquet.MemoryRecord) wire.ExportMemory {
	var attrs map[string]string
	if r.Attrs != "" {
		_ = json.Unmarshal([]byte(r.Attrs), &attrs)
	}
	return wire.ExportMemory{
		ID:             r.ID,
		Layer:          int(r.Layer),
		Content:        r.Content,
		ContentHash:    r.ContentHash,
		Embedding:      vector.FromBytes(r.Embedding),
		Importance:     r.Importance,
		AccessCount:    int(r.AccessCount),
		CreatedAt:      r.CreatedAt,
		UpdatedAt:      r.UpdatedAt,
		LastAccessedAt: r.LastAccessedAt,
		DecayRate:      r.DecayRate,
		Stability:      r.Stability,
		Source:         "server",
		Agent:          r.Agent,
		Valence:        r.Valence,
		ValenceScored:  r.ValenceScored,
		EmbeddingModel: r.EmbeddingModel,
		Attrs:          attrs,
	}
}

func writeMemoryJSONL(zw *zip.Writer, filename string, records []wire.ExportMemory) error {
	ww, err := zw.Create(filename)
	if err != nil {
		return fmt.Errorf("create zip entry %s: %w", filename, err)
	}
	for i := range records {
		b, err := json.Marshal(&records[i])
		if err != nil {
			return fmt.Errorf("marshal memory: %w", err)
		}
		b = append(b, '\n')
		if _, err := ww.Write(b); err != nil {
			return fmt.Errorf("write memory: %w", err)
		}
	}
	return nil
}

func writeZIPEntry(zw *zip.Writer, filename string, v any) error {
	ww, err := zw.Create(filename)
	if err != nil {
		return fmt.Errorf("create zip entry %s: %w", filename, err)
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal %s: %w", filename, err)
	}
	var buf bytes.Buffer
	buf.Write(b)
	_, err = buf.WriteTo(ww)
	return err
}
