// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package memory

import (
	"context"
	"crypto/sha256"
	"encoding/json"
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

	// Async importance scoring and tag extraction.
	s.wg.Add(1)
	go s.scoreAndTagAsync(memID, content)

	m := rowToMemory(row, attrs)
	m.Embedding = embF32
	return m, nil
}

// scoreAndTagAsync calls the LLM to score importance, extract tags, and score valence for a memory.
// It updates the DB with the results. Non-fatal on any error.
func (s *MemoryStore) scoreAndTagAsync(memID, content string) {
	defer s.wg.Done()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Score importance.
	importancePromptStr := fmt.Sprintf(
		"Rate the importance of this memory for future reference on a scale of 1 to 10.\n"+
			"Respond with ONLY a single integer 1-10.\nMemory: %s", content)

	resp, err := s.llm.Complete(ctx, importancePromptStr)
	if err == nil {
		if score, parseErr := parseImportance(resp); parseErr == nil {
			// Best-effort update; ignore errors on shutdown.
			_ = s.db.UpdateImportance(context.Background(), memID, score)
		}
	}

	// Extract tags.
	tagResp, err := s.llm.Complete(ctx, tagPrompt(content))
	if err == nil {
		if tags := parseTags(tagResp); tags != "" {
			_ = s.db.MergeAttribute(context.Background(), memID, "tags", tags)
		}
	}

	// Score valence.
	valResp, err := s.llm.Complete(ctx, valencePrompt(content))
	if err == nil {
		if v, parseErr := parseValence(valResp); parseErr == nil {
			_ = s.db.UpdateValence(context.Background(), memID, v)
		}
	}
}

// tagPrompt returns a prompt that asks the LLM to extract tags from the given content.
func tagPrompt(content string) string {
	return `Extract 3-5 relevant tags from this text as a JSON array of lowercase strings.
Tags should describe topics, technologies, error types, or key concepts.
Respond with ONLY a valid JSON array, e.g.: ["go", "error-handling", "nil-pointer"]
Text: ` + content
}

// parseTags parses a JSON array of tags from an LLM response and returns
// a comma-separated string of up to 5 sanitized lowercase tags.
// Returns "" if parsing fails or no valid tags are found.
func parseTags(response string) string {
	response = strings.TrimSpace(response)
	response = strings.TrimPrefix(response, "```json")
	response = strings.TrimPrefix(response, "```")
	response = strings.TrimSuffix(response, "```")
	response = strings.TrimSpace(response)

	var tags []string
	if err := json.Unmarshal([]byte(response), &tags); err != nil {
		return ""
	}

	// Sanitize: lowercase, trim spaces, max 5 tags.
	clean := make([]string, 0, 5)
	for _, tag := range tags {
		tag = strings.ToLower(strings.TrimSpace(tag))
		if tag != "" {
			clean = append(clean, tag)
		}
		if len(clean) == 5 {
			break
		}
	}
	return strings.Join(clean, ",")
}

// valencePrompt returns a prompt asking the LLM to rate the outcome valence of a memory.
func valencePrompt(content string) string {
	return `Rate this memory's outcome valence. Reply with ONLY a single number, nothing else.
Scale: -1.0 (total failure/negative) to 0.0 (neutral/no outcome) to +1.0 (great success/positive).
Examples of valid replies: 0.8   -0.5   0.0   -1.0   0.3
Memory: ` + content
}

// parseValence extracts a float from an LLM valence response and clamps it to [-1.0, 1.0].
// It tolerates verbose responses by scanning for the last valid float in the text.
func parseValence(response string) (float32, error) {
	// Try direct parse first (ideal case).
	s := strings.TrimSpace(response)
	if v, err := strconv.ParseFloat(s, 32); err == nil {
		return clampValence(float32(v)), nil
	}
	// Claude sometimes adds explanation after the number — scan tokens for the first valid float.
	fields := strings.Fields(s)
	for _, field := range fields {
		token := strings.Trim(field, ".*:,()+")
		if v, err := strconv.ParseFloat(token, 32); err == nil {
			return clampValence(float32(v)), nil
		}
	}
	return 0.0, fmt.Errorf("no valid float in valence response %q", s)
}

func clampValence(v float32) float32 {
	if v > 1.0 {
		return 1.0
	}
	if v < -1.0 {
		return -1.0
	}
	return v
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
// mergeSourceIntoAttrs returns attrs with source/agent added if not already present.
func mergeSourceIntoAttrs(attrs map[string]string, source, agent string) map[string]string {
	if source == "" && agent == "" {
		return attrs
	}
	merged := make(map[string]string, len(attrs)+2)
	for k, v := range attrs {
		merged[k] = v
	}
	if source != "" {
		merged["source"] = source
	}
	if agent != "" {
		merged["agent"] = agent
	}
	return merged
}

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
		Attrs:          mergeSourceIntoAttrs(attrs, row.Source, row.Agent),
		Valence:        row.Valence,
		ValenceScored:  row.ValenceScored,
	}
	if len(row.Embedding) > 0 {
		m.Embedding = vector.FromBytes(row.Embedding)
	}
	return m
}
