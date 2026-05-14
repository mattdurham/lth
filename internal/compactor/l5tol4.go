// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package compactor

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/mattdurham/lth/internal/memory"
)

const windowSize = 20

// compactL5toL4 promotes L5 memories to L4 via LLM summarization.
// Triggers when CountByLayer(5) > L5Threshold OR oldest L5 > L5MaxAge hours.
func (c *Compactor) compactL5toL4(ctx context.Context) (int, error) {
	// Check trigger conditions.
	stats, err := c.store.Stats(ctx)
	if err != nil {
		return 0, fmt.Errorf("stats: %w", err)
	}

	l5Count := stats.ByLayer[5]
	if l5Count == 0 {
		return 0, nil
	}

	// Load L5 memories.
	l5Memories, err := c.store.ListLayer(ctx, 5)
	if err != nil {
		return 0, fmt.Errorf("list L5: %w", err)
	}
	if len(l5Memories) == 0 {
		return 0, nil
	}

	// Check trigger: count or age.
	threshold := c.cfg.Compaction.L5Threshold
	maxAgeH := c.cfg.Compaction.L5MaxAgeH
	now := time.Now().UTC()

	var triggered bool
	if l5Count > threshold {
		triggered = true
	}
	if !triggered {
		for _, m := range l5Memories {
			if now.Sub(m.CreatedAt).Hours() > float64(maxAgeH) {
				triggered = true
				break
			}
		}
	}
	if !triggered {
		return 0, nil
	}

	// Chunk into windows of windowSize.
	promoted := 0
	for i := 0; i < len(l5Memories); i += windowSize {
		end := i + windowSize
		if end > len(l5Memories) {
			end = len(l5Memories)
		}
		window := l5Memories[i:end]

		n, err := c.summarizeWindow(ctx, window, now)
		if err != nil {
			// LLM failure: skip this window, continue.
			c.logger.Warn("L5→L4 window summarization failed", "err", err, "window_start", i)
			continue
		}
		promoted += n
	}

	return promoted, nil
}

// summarizeWindow calls LLM to summarize a window of L5 memories and stores the result as L4.
func (c *Compactor) summarizeWindow(ctx context.Context, window []*memory.Memory, now time.Time) (int, error) {
	if len(window) == 0 {
		return 0, nil
	}

	// Build prompt.
	var sb strings.Builder
	sb.WriteString("Summarize these raw observations into 1-3 key insights for future reference.\n")
	sb.WriteString("Focus on decisions made, problems encountered, and solutions found.\n")
	sb.WriteString("Observations:\n")
	for _, m := range window {
		sb.WriteString("- ")
		sb.WriteString(m.Content)
		sb.WriteByte('\n')
	}

	llmCtx, cancel := context.WithTimeout(ctx, time.Duration(c.cfg.LLM.TimeoutS)*time.Second)
	defer cancel()

	summary, err := c.llm.Complete(llmCtx, sb.String())
	if err != nil {
		return 0, fmt.Errorf("LLM summarize: %w", err)
	}

	// Store summary as L4 memory.
	attrs := map[string]string{
		"source":       "compactor",
		"window_start": window[0].CreatedAt.Format(time.RFC3339),
		"window_end":   window[len(window)-1].CreatedAt.Format(time.RFC3339),
	}
	_, err = c.store.Store(ctx, 4, summary, attrs)
	if err != nil {
		return 0, fmt.Errorf("store L4 summary: %w", err)
	}

	// Soft-delete all L5 memories in the window.
	ids := make([]string, len(window))
	for i, m := range window {
		ids[i] = m.ID
	}
	if err := c.store.SoftDelete(ctx, ids, "compacted to L4"); err != nil {
		return 0, fmt.Errorf("soft delete L5 window: %w", err)
	}

	_ = now // used for logging context in caller
	return 1, nil // 1 L4 memory created per window
}
