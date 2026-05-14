// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package memory

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mattdurham/lth/internal/config"
	"github.com/mattdurham/lth/internal/db"
	"github.com/mattdurham/lth/internal/graph"
	"github.com/mattdurham/lth/internal/llm"
	"github.com/mattdurham/lth/internal/vector"
)

// decayRates maps layer → base decay rate.
var decayRates = map[int]float32{
	1: 0.0,
	2: 0.01,
	3: 0.05,
	4: 0.1,
	5: 0.5,
}

// NewMemoryStore creates and returns a MemoryStore. It calls graph.LoadAll on startup.
func NewMemoryStore(d *db.DB, emb vector.Embedder, l llm.LLM, g *graph.Graph, cfg *config.Config) (*MemoryStore, error) {
	s := &MemoryStore{
		db:    d,
		emb:   emb,
		llm:   l,
		graph: g,
		cfg:   cfg,
	}

	if err := g.LoadAll(context.Background()); err != nil {
		return nil, fmt.Errorf("load graph: %w", err)
	}

	return s, nil
}

// Close waits for all in-flight async goroutines to complete.
func (s *MemoryStore) Close() {
	s.wg.Wait()
}

// Store stores a memory, handling deduplication, embedding, and auto-linking.
func (s *MemoryStore) Store(ctx context.Context, layer int, content string, attrs map[string]string) (*Memory, error) {
	if layer < 1 || layer > 5 {
		return nil, fmt.Errorf("invalid layer %d: must be 1-5", layer)
	}
	if content == "" {
		return nil, fmt.Errorf("content must not be empty")
	}

	// Compute content hash for deduplication.
	h := sha256.Sum256([]byte(content))
	hash := fmt.Sprintf("%x", h)

	// Check for existing memory with same hash.
	existing, err := s.db.GetByHash(ctx, hash)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("check hash: %w", err)
	}
	if existing != nil {
		return rowToMemory(existing, attrs), nil
	}

	// Generate embedding (optional — null if embedder fails).
	var embBytes []byte
	var embF32 []float32
	if s.emb != nil {
		embF32, err = s.emb.Embed(ctx, content)
		if err == nil && len(embF32) > 0 {
			embBytes = vector.ToBytes(embF32)
		}
		// On error: store without embedding (degrades to FTS5-only search).
	}

	now := time.Now().UTC()
	memID := uuid.New().String()
	baseDecay := decayRates[layer]

	row := &db.MemoryRow{
		ID:             memID,
		Layer:          layer,
		Content:        content,
		ContentHash:    hash,
		Embedding:      embBytes,
		Importance:     5.0, // default until async LLM scores it
		AccessCount:    0,
		CreatedAt:      now,
		UpdatedAt:      now,
		LastAccessedAt: now,
		DecayRate:      baseDecay,
		Stability:      1.0,
		Source:         attrOrEmpty(attrs, "source"),
		Agent:          attrOrEmpty(attrs, "agent"),
	}

	if err := s.db.InsertMemory(ctx, row); err != nil {
		return nil, fmt.Errorf("insert memory: %w", err)
	}

	// Store attributes.
	if len(attrs) > 0 {
		if err := s.db.SetAttributes(ctx, memID, attrs); err != nil {
			return nil, fmt.Errorf("set attributes: %w", err)
		}
	}

	// Auto-link via Zettelkasten (synchronous, uses vec0 KNN).
	if len(embF32) > 0 {
		if err := s.graph.AutoLink(ctx, memID, embF32); err != nil {
			// Non-fatal: auto-link failure doesn't prevent store.
			_ = err
		}
	}

	// Async importance scoring.
	s.wg.Add(1)
	go s.scoreImportanceAsync(memID, content)

	m := rowToMemory(row, attrs)
	m.Embedding = embF32
	return m, nil
}

// scoreImportanceAsync calls the LLM to score the importance of a memory.
// It updates the DB with the result. Non-fatal on any error.
func (s *MemoryStore) scoreImportanceAsync(memID, content string) {
	defer s.wg.Done()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	prompt := fmt.Sprintf(
		"Rate the importance of this memory for future reference on a scale of 1 to 10.\n"+
			"Respond with ONLY a single integer 1-10.\nMemory: %s", content)

	resp, err := s.llm.Complete(ctx, prompt)
	if err != nil {
		return
	}

	score, err := parseImportance(resp)
	if err != nil {
		return
	}

	// Best-effort update; ignore errors on shutdown.
	_ = s.db.UpdateImportance(context.Background(), memID, score)
}

// parseImportance parses a 1-10 integer from an LLM response string.
func parseImportance(resp string) (float32, error) {
	resp = strings.TrimSpace(resp)
	n, err := strconv.Atoi(resp)
	if err != nil {
		// Try to find a digit in the response.
		for _, r := range resp {
			if r >= '1' && r <= '9' {
				n = int(r - '0')
				err = nil
				break
			}
		}
		if err != nil {
			return 5.0, fmt.Errorf("parse importance %q: %w", resp, err)
		}
	}
	if n < 1 {
		n = 1
	}
	if n > 10 {
		n = 10
	}
	return float32(n), nil
}

// attrOrEmpty returns attrs[key] if present, otherwise "".
func attrOrEmpty(attrs map[string]string, key string) string {
	if attrs == nil {
		return ""
	}
	return attrs[key]
}

// rowToMemory converts a db.MemoryRow to a memory.Memory with the given attributes.
func rowToMemory(row *db.MemoryRow, attrs map[string]string) *Memory {
	m := &Memory{
		ID:             row.ID,
		Layer:          row.Layer,
		Content:        row.Content,
		ContentHash:    row.ContentHash,
		Importance:     row.Importance,
		AccessCount:    row.AccessCount,
		CreatedAt:      row.CreatedAt,
		UpdatedAt:      row.UpdatedAt,
		LastAccessedAt: row.LastAccessedAt,
		DecayRate:      row.DecayRate,
		Stability:      row.Stability,
		Source:         row.Source,
		Agent:          row.Agent,
		CompactedAt:    row.CompactedAt,
		Attrs:          attrs,
	}
	if len(row.Embedding) > 0 {
		m.Embedding = vector.FromBytes(row.Embedding)
	}
	return m
}
