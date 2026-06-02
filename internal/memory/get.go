// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package memory

import (
	"context"
	"fmt"
	"time"

	"github.com/mattdurham/lth/internal/vector"
)

// Get retrieves a memory by ID, increments access_count, and updates Ebbinghaus stability.
func (s *MemoryStore) Get(ctx context.Context, id string) (*Memory, error) {
	row, err := s.db.GetMemory(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get memory: %w", err)
	}

	now := time.Now().UTC()
	if err := s.db.MarkAccessed(ctx, id, now); err != nil {
		return nil, fmt.Errorf("mark accessed: %w", err)
	}

	// Ebbinghaus stability update.
	newStability := row.Stability + 1.0
	baseDecay := decayRates[row.Layer]
	newDecayRate := baseDecay
	if row.Layer > 1 && newStability > 0 {
		newDecayRate = baseDecay / newStability
	}
	if err := s.db.UpdateStability(ctx, id, newStability, newDecayRate); err != nil {
		return nil, fmt.Errorf("update stability: %w", err)
	}
	row.AccessCount++
	row.Stability = newStability
	row.DecayRate = newDecayRate
	row.LastAccessedAt = now

	attrs, err := s.db.GetAttributes(ctx, row.ID)
	if err != nil {
		return nil, fmt.Errorf("get attributes: %w", err)
	}

	m := rowToMemory(row, attrs)
	if len(row.Embedding) > 0 {
		m.Embedding = vector.FromBytes(row.Embedding)
	}
	return m, nil
}

// ListLayer returns all active memories in the given layer.
func (s *MemoryStore) ListLayer(ctx context.Context, layer int) ([]*Memory, error) {
	rows, err := s.db.ListLayer(ctx, layer, true)
	if err != nil {
		return nil, fmt.Errorf("list layer: %w", err)
	}

	result := make([]*Memory, 0, len(rows))
	for _, row := range rows {
		attrs, err := s.db.GetAttributes(ctx, row.ID)
		if err != nil {
			return nil, fmt.Errorf("get attributes for %s: %w", row.ID, err)
		}
		m := rowToMemory(row, attrs)
		result = append(result, m)
	}
	return result, nil
}

// SoftDelete marks the given memory IDs as compacted. It does not hard-delete any rows.
func (s *MemoryStore) SoftDelete(ctx context.Context, ids []string, reason string) error {
	now := time.Now().UTC()
	for _, id := range ids {
		if err := s.db.SoftDelete(ctx, id, now); err != nil {
			return fmt.Errorf("soft delete %s: %w", id, err)
		}
	}
	return nil
}

// Stats returns aggregate statistics about the memory store.
func (s *MemoryStore) Stats(ctx context.Context) (*Stats, error) {
	dbStats, err := s.db.Stats(ctx)
	if err != nil {
		return nil, fmt.Errorf("stats: %w", err)
	}
	return &Stats{
		TotalMemories: dbStats.TotalMemories,
		ByLayer:       dbStats.ByLayer,
		TotalEdges:    dbStats.TotalEdges,
	}, nil
}

// DistinctAttrValues returns all distinct values for a given attribute key across all memories.
func (s *MemoryStore) DistinctAttrValues(ctx context.Context, key string) ([]string, error) {
	return s.db.DistinctAttrValues(ctx, key)
}
