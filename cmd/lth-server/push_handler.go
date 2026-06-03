// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package main

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mattdurham/lth/internal/parquet"
	"github.com/mattdurham/lth/internal/vector"
	"github.com/mattdurham/lth/internal/wire"
)

// PushHandler handles POST /v1/sync/push.

func (h *PushHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
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

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
		return
	}

	accepted, skipped, err := h.processPush(r.Context(), account, org, user, team, body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(pushResponse{Accepted: accepted, Skipped: skipped})
}

func (h *PushHandler) processPush(ctx context.Context, account, org, user, team string, body []byte) (accepted, skipped int, err error) {
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return 0, 0, fmt.Errorf("parse zip: %w", err)
	}

	manifest, err := readManifestFromZip(zr)
	if err != nil {
		return 0, 0, err
	}

	byLayer := make(map[int][]parquet.MemoryRecord)

	for _, filename := range manifest.Files {
		if !strings.HasPrefix(filename, "memories_") {
			continue
		}
		zf := findInZip(zr, filename)
		if zf == nil {
			return 0, 0, fmt.Errorf("file %q in manifest not found in archive", filename)
		}
		rc, err := zf.Open()
		if err != nil {
			return 0, 0, fmt.Errorf("open %q: %w", filename, err)
		}
		n, skip, recs, parseErr := h.parseMemoryFile(rc)
		rc.Close() //nolint:errcheck
		if parseErr != nil {
			return 0, 0, fmt.Errorf("parse %s: %w", filename, parseErr)
		}
		accepted += n
		skipped += skip
		for _, rec := range recs {
			byLayer[int(rec.Layer)] = append(byLayer[int(rec.Layer)], rec)
		}
	}

	if !h.cfg.Parquet.Enabled {
		return accepted, skipped, nil
	}

	date := time.Now().UTC().Format("2006-01-02")
	pushID := uuid.NewString()

	for layer, recs := range byLayer {
		// Dedup: check content_hash index, skip already-seen memories.
		// Attrs are always written/updated even on a dedup hit — content is
		// immutable but attrs are mutable metadata that can change over time.
		var fresh []parquet.MemoryRecord
		for _, rec := range recs {
			if rec.Attrs != "" {
				attrsKey := fmt.Sprintf("%s/%s/attrs/%s/%s", account, org, rec.ContentHash[:2], rec.ContentHash)
				_ = h.store.Put(ctx, attrsKey, strings.NewReader(rec.Attrs))
			}
			indexKey := fmt.Sprintf("%s/%s/index/%s/%s", account, org, rec.ContentHash[:2], rec.ContentHash)
			exists, checkErr := h.store.Exists(ctx, indexKey)
			if checkErr != nil || exists {
				skipped++
				accepted-- // was counted in parseMemoryFile, undo it
				continue
			}
			fresh = append(fresh, rec)
		}
		if len(fresh) == 0 {
			continue
		}

		scope := layerScope(layer, user, team)
		key := fmt.Sprintf("%s/%s/%s/L%d/date=%s/%s.parquet", account, org, scope, layer, date, pushID)
		var buf bytes.Buffer
		if writeErr := h.writer.Write(ctx, &buf, fresh); writeErr != nil {
			return 0, 0, fmt.Errorf("write parquet L%d: %w", layer, writeErr)
		}
		if putErr := h.store.Put(ctx, key, &buf); putErr != nil {
			return 0, 0, fmt.Errorf("store parquet L%d: %w", layer, putErr)
		}

		// Write dedup index markers for accepted memories.
		for _, rec := range fresh {
			indexKey := fmt.Sprintf("%s/%s/index/%s/%s", account, org, rec.ContentHash[:2], rec.ContentHash)
			_ = h.store.Put(ctx, indexKey, bytes.NewReader([]byte{}))
		}
	}

	return accepted, skipped, nil
}

func (h *PushHandler) parseMemoryFile(rc io.Reader) (accepted, skipped int, recs []parquet.MemoryRecord, err error) {
	scanner := bufio.NewScanner(rc)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var em wire.ExportMemory
		if err := json.Unmarshal(line, &em); err != nil {
			return accepted, skipped, recs, fmt.Errorf("decode memory: %w", err)
		}
		if em.Source == "server" {
			skipped++
			continue
		}
		rec := exportMemoryToRecord(em)
		recs = append(recs, rec)
		accepted++
	}
	return accepted, skipped, recs, scanner.Err()
}

func exportMemoryToRecord(em wire.ExportMemory) parquet.MemoryRecord {
	var attrsJSON string
	if len(em.Attrs) > 0 {
		if b, err := json.Marshal(em.Attrs); err == nil {
			attrsJSON = string(b)
		}
	}
	return parquet.MemoryRecord{
		ID:             em.ID,
		Layer:          int32(em.Layer),
		Content:        em.Content,
		ContentHash:    em.ContentHash,
		Embedding:      vector.ToBytes(em.Embedding),
		Importance:     em.Importance,
		AccessCount:    int32(em.AccessCount),
		CreatedAt:      em.CreatedAt,
		UpdatedAt:      em.UpdatedAt,
		LastAccessedAt: em.LastAccessedAt,
		DecayRate:      em.DecayRate,
		Stability:      em.Stability,
		Source:         "server",
		Agent:          em.Agent,
		Valence:        em.Valence,
		ValenceScored:  em.ValenceScored,
		EmbeddingModel: em.EmbeddingModel,
		Attrs:          attrsJSON,
	}
}

func layerScope(layer int, user, team string) string {
	switch layer {
	case 1, 2:
		return "users/" + user
	case 3:
		return "shared"
	case 4:
		if team != "" {
			return "teams/" + team
		}
		return "shared"
	default:
		return "users/" + user
	}
}

func readManifestFromZip(zr *zip.Reader) (wire.ExportManifest, error) {
	f := findInZip(zr, "manifest.json")
	if f == nil {
		return wire.ExportManifest{}, fmt.Errorf("manifest.json not found in archive")
	}
	rc, err := f.Open()
	if err != nil {
		return wire.ExportManifest{}, fmt.Errorf("open manifest.json: %w", err)
	}
	defer rc.Close() //nolint:errcheck
	var m wire.ExportManifest
	if err := json.NewDecoder(rc).Decode(&m); err != nil {
		return wire.ExportManifest{}, fmt.Errorf("decode manifest: %w", err)
	}
	return m, nil
}

func findInZip(zr *zip.Reader, name string) *zip.File {
	for _, f := range zr.File {
		if f.Name == name {
			return f
		}
	}
	return nil
}
