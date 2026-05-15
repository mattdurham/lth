// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package graph

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/mattdurham/lth/internal/vector"
)

const (
	autolinkK         = 5
	autolinkThreshold = float32(0.75)
)

// AutoLink performs Zettelkasten-style auto-linking for the given memory.
// It queries vec0 for the K nearest neighbors, computes exact cosine similarity
// for each candidate, and creates "relates_to" edges for those above 0.75 cosine.
func (g *Graph) AutoLink(ctx context.Context, memID string, emb []float32) error {
	if len(emb) == 0 {
		return nil
	}

	// Find K nearest neighbors via vec0.
	candidates, err := g.db.VectorSearch(ctx, emb, nil, autolinkK+1) // +1 to exclude self
	if err != nil {
		return fmt.Errorf("vector search for autolink: %w", err)
	}

	for _, c := range candidates {
		if c.ID == memID {
			continue // skip self
		}
		if len(c.Embedding) == 0 {
			continue
		}

		// Compute exact cosine similarity.
		candEmb := vector.FromBytes(c.Embedding)
		cos := vector.Cosine(emb, candEmb)
		if cos < autolinkThreshold {
			continue
		}

		edge := &Edge{
			ID:       uuid.New().String(),
			FromID:   memID,
			ToID:     c.ID,
			EdgeType: "relates_to",
			Weight:   cos,
			Created:  time.Now().UTC(),
		}
		if err := g.AddEdge(ctx, edge); err != nil {
			return fmt.Errorf("autolink add edge %s→%s: %w", memID, c.ID, err)
		}
	}

	return nil
}
