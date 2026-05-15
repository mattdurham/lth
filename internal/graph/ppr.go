// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package graph

// PPR runs Personalized PageRank on the in-memory adjacency cache.
// seeds is a list of memory IDs to use as the personalization vector.
// d is the damping factor (typically 0.85). iters is the number of iterations.
// Returns a map of memory ID → PPR score.
func (g *Graph) PPR(seeds []string, d float64, iters int) map[string]float64 {
	if len(seeds) == 0 {
		return nil
	}

	g.mu.RLock()
	defer g.mu.RUnlock()

	// Build personalization vector.
	seedWeight := 1.0 / float64(len(seeds))
	p := make(map[string]float64, len(seeds))
	for _, s := range seeds {
		p[s] = seedWeight
	}

	// Initialize rank vector to personalization.
	r := make(map[string]float64, len(p))
	for k, v := range p {
		r[k] = v
	}

	// Iterate.
	for i := 0; i < iters; i++ {
		next := make(map[string]float64, len(r))

		// Teleportation component: (1-d)*p[v] for seeds.
		for v, pv := range p {
			next[v] += (1 - d) * pv
		}

		// Propagation component: d * sum(r[u] * w(u,v) / outdegree(u)).
		for u, ru := range r {
			neighbors := g.adj[u]
			if len(neighbors) == 0 {
				// No outgoing edges: teleport (dangling node).
				for v := range p {
					next[v] += d * ru * seedWeight
				}
				continue
			}

			// Compute total outgoing weight for normalization.
			var totalWeight float64
			for _, n := range neighbors {
				if n.outgoing {
					totalWeight += float64(n.weight)
				}
			}
			if totalWeight == 0 {
				// All edges are incoming; treat as dangling.
				for v := range p {
					next[v] += d * ru * seedWeight
				}
				continue
			}

			for _, n := range neighbors {
				if n.outgoing {
					next[n.neighborID] += d * ru * float64(n.weight) / totalWeight
				}
			}
		}

		r = next
	}

	return r
}
