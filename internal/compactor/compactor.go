// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// Package compactor provides compaction scheduling and strategies for promoting memories across layers.
package compactor

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/mattdurham/lth/internal/config"
	"github.com/mattdurham/lth/internal/graph"
	"github.com/mattdurham/lth/internal/llm"
	"github.com/mattdurham/lth/internal/memory"
)

// Compactor orchestrates memory compaction across layers.
type Compactor struct {
	store  memory.Store
	llm    llm.LLM
	graph  *graph.Graph
	cfg    *config.Config
	logger *slog.Logger
}

// New creates a new Compactor.
func New(store memory.Store, l llm.LLM, g *graph.Graph, cfg *config.Config, logger *slog.Logger) *Compactor {
	if logger == nil {
		logger = slog.Default()
	}
	return &Compactor{
		store:  store,
		llm:    l,
		graph:  g,
		cfg:    cfg,
		logger: logger,
	}
}

// Run runs compaction on a ticker until the context is canceled.
// It never returns nil except on context cancellation.
func (c *Compactor) Run(ctx context.Context, interval time.Duration) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			report, err := c.RunOnce(ctx)
			if err != nil {
				c.logger.Warn("compaction run error", "err", err)
				continue
			}
			c.logger.Info("compaction complete",
				"l5_to_l4", report.L5toL4,
				"l4_to_l3", report.L4toL3,
				"l3_to_l2", report.L3toL2,
				"errors", len(report.Errors),
			)
		}
	}
}

// RunOnce runs all three compaction paths once and returns a report.
func (c *Compactor) RunOnce(ctx context.Context) (*CompactionReport, error) {
	report := &CompactionReport{}

	n, err := c.compactL5toL4(ctx)
	if err != nil {
		return report, fmt.Errorf("L5→L4 compaction: %w", err)
	}
	report.L5toL4 = n

	n, err = c.compactL4toL3(ctx)
	if err != nil {
		return report, fmt.Errorf("L4→L3 compaction: %w", err)
	}
	report.L4toL3 = n

	n, err = c.compactL3toL2(ctx)
	if err != nil {
		return report, fmt.Errorf("L3→L2 compaction: %w", err)
	}
	report.L3toL2 = n

	// Rebuild adjacency cache per graph.SPECS.md invariant 6.
	if report.L5toL4 > 0 || report.L4toL3 > 0 || report.L3toL2 > 0 {
		if err := c.graph.LoadAll(ctx); err != nil {
			report.Errors = append(report.Errors, fmt.Errorf("reload graph: %w", err))
		}
	}

	return report, nil
}
