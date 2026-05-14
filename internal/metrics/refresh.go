// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package metrics

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/mattdurham/lth/internal/memory"
)

// RefreshLoop periodically updates gauge metrics from the store.
// Run as a goroutine alongside the daemon. Stops when ctx is canceled.
func RefreshLoop(ctx context.Context, store memory.Store, m *Metrics, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	refresh := func() {
		stats, err := store.Stats(ctx)
		if err != nil {
			slog.Warn("metrics refresh failed", "err", err)
			return
		}
		for layer := 1; layer <= 5; layer++ {
			m.MemoriesTotal.WithLabelValues(fmt.Sprintf("%d", layer)).Set(float64(stats.ByLayer[layer]))
		}
		m.GraphEdgesTotal.Set(float64(stats.TotalEdges))
	}

	refresh() // immediate first refresh
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			refresh()
		}
	}
}
