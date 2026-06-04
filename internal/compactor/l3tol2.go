// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package compactor

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mattdurham/lth/internal/graph"
)

// wisdomResponse is the structured output from the L3→L2 promotion prompt.
type wisdomResponse struct {
	Rule   string   `json:"rule"`
	Tags   []string `json:"tags"`
	Domain string   `json:"domain"`
}

// compactL3toL2 promotes eligible L3 memories to L2 via LLM pattern recognition.
// Triggers when access_count >= L3EpisodesMin AND importance > L3ImportanceMin.
// L3 memories are NOT soft-deleted after promotion (they remain active).
func (c *Compactor) compactL3toL2(ctx context.Context) (int, error) {
	l3Memories, err := c.store.ListLayer(ctx, 3)
	if err != nil {
		return 0, fmt.Errorf("list L3: %w", err)
	}
	if len(l3Memories) == 0 {
		return 0, nil
	}

	minEpisodes := c.cfg.Compaction.L3EpisodesMin
	minImportance := c.cfg.Compaction.L3ImportanceMin

	promoted := 0
	for _, m := range l3Memories {
		if m.AccessCount < minEpisodes || m.Importance <= minImportance {
			continue
		}

		// Check if this L3 already has a derived L2 (via neighbors).
		neighbors := c.graph.Neighbors(m.ID, []string{"derived_from"})
		if len(neighbors) > 0 {
			continue // already promoted
		}

		n, err := c.promoteToL2(ctx, m.ID, m.Content)
		if err != nil {
			c.logger.Warn("L3→L2 promotion failed", "err", err, "memory_id", m.ID)
			continue
		}
		promoted += n
	}

	return promoted, nil
}

// wisdomPrompt builds the L3→L2 promotion prompt.
func wisdomPrompt(content string) string {
	return `You are distilling a repeated engineering skill into a concise behavioral rule for an AI agent memory system.

Given the skill below, respond with ONLY a valid JSON object (no markdown, no code fences) with these fields:
- "rule": one actionable imperative sentence, max 20 words
- "tags": array of 3-5 lowercase topic/technology strings
- "domain": single lowercase slug (e.g. "coding", "ops", "email", "research", "writing", "general")

Skill:
` + content
}

// parseWisdom parses the structured JSON response from the L3→L2 promotion LLM call.
func parseWisdom(resp string) (*wisdomResponse, error) {
	resp = strings.TrimSpace(resp)
	resp = strings.TrimPrefix(resp, "```json")
	resp = strings.TrimPrefix(resp, "```")
	resp = strings.TrimSuffix(resp, "```")
	resp = strings.TrimSpace(resp)

	var w wisdomResponse
	if err := json.Unmarshal([]byte(resp), &w); err != nil {
		return nil, fmt.Errorf("unmarshal wisdom: %w", err)
	}
	return &w, nil
}

// formatWisdom renders a wisdomResponse as the L2 memory content string.
func formatWisdom(w *wisdomResponse) string {
	return strings.TrimSpace(w.Rule)
}

// promoteToL2 creates an L2 memory from a single L3 memory.
func (c *Compactor) promoteToL2(ctx context.Context, sourceID, content string) (int, error) {
	llmCtx, cancel := context.WithTimeout(ctx, time.Duration(c.cfg.LLM.TimeoutS)*time.Second)
	defer cancel()

	resp, err := c.llm.Complete(llmCtx, wisdomPrompt(content))
	if err != nil {
		return 0, fmt.Errorf("LLM rule extraction: %w", err)
	}

	w, err := parseWisdom(resp)
	if err != nil {
		return 0, fmt.Errorf("parse wisdom: %w", err)
	}

	tags := strings.Join(w.Tags, ",")
	attrs := map[string]string{
		"source":         "compactor",
		"tags":           tags,
		"lth_classified": "1",
	}
	if w.Domain != "" {
		attrs["domain"] = strings.ToLower(strings.TrimSpace(w.Domain))
	}
	l2, err := c.store.Store(ctx, 2, formatWisdom(w), attrs)
	if err != nil {
		return 0, fmt.Errorf("store L2: %w", err)
	}

	// Add derived_from edge: L2 → L3.
	e := &graph.Edge{
		ID:       uuid.New().String(),
		FromID:   l2.ID,
		ToID:     sourceID,
		EdgeType: "derived_from",
		Weight:   1.0,
		Created:  time.Now().UTC(),
	}
	if err := c.graph.AddEdge(ctx, e); err != nil {
		c.logger.Warn("failed to add derived_from edge", "err", err)
	}

	// L3 is NOT soft-deleted — it remains active.
	return 1, nil
}
