// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/mattdurham/lth/internal/blobstore"
	"github.com/mattdurham/lth/internal/parquet"
)

// ObserveHandler handles POST /v1/observations (L5 write-only stream).
type ObserveHandler struct {
	store  blobstore.BlobStore
	writer *parquet.Writer
}

type observationRecord struct {
	Content   string            `json:"content"`
	Agent     string            `json:"agent,omitempty"`
	Attrs     map[string]string `json:"attrs,omitempty"`
	Valence   float32           `json:"valence"`
	CreatedAt time.Time         `json:"created_at,omitempty"`
}

func (h *ObserveHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	account := r.Header.Get("X-LTH-Account")
	org := r.Header.Get("X-LTH-Org")
	user := r.Header.Get("X-LTH-User")
	if account == "" || org == "" || user == "" {
		http.Error(w, "X-LTH-Account, X-LTH-Org, X-LTH-User headers required", http.StatusBadRequest)
		return
	}

	var recs []parquet.MemoryRecord
	scanner := bufio.NewScanner(r.Body)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var obs observationRecord
		if err := json.Unmarshal(line, &obs); err != nil {
			http.Error(w, "decode observation: "+err.Error(), http.StatusBadRequest)
			return
		}
		createdAt := obs.CreatedAt
		if createdAt.IsZero() {
			createdAt = time.Now().UTC()
		}
		recs = append(recs, parquet.MemoryRecord{
			ID:          uuid.NewString(),
			Layer:       5,
			Content:     obs.Content,
			ContentHash: fmt.Sprintf("%x", simpleHash(obs.Content)),
			Source:      "server",
			Agent:       obs.Agent,
			Valence:     obs.Valence,
			CreatedAt:   createdAt,
			UpdatedAt:   createdAt,
		})
	}
	if err := scanner.Err(); err != nil {
		http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
		return
	}

	if len(recs) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if err := h.writeObservations(r.Context(), account, org, user, recs); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *ObserveHandler) writeObservations(ctx context.Context, account, org, user string, recs []parquet.MemoryRecord) error {
	date := time.Now().UTC().Format("2006-01-02")
	streamID := uuid.NewString()
	key := fmt.Sprintf("%s/%s/L5/users/%s/date=%s/%s.parquet", account, org, user, date, streamID)

	var buf bytes.Buffer
	if err := h.writer.Write(ctx, &buf, recs); err != nil {
		return fmt.Errorf("write parquet: %w", err)
	}
	return h.store.Put(ctx, key, &buf)
}

// simpleHash produces a deterministic hash for a string for use as a content_hash placeholder.
func simpleHash(s string) uint64 {
	var h uint64 = 14695981039346656037
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= 1099511628211
	}
	return h
}