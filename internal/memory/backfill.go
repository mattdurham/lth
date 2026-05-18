// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package memory

import (
	"context"
	"log/slog"
	"time"

	"github.com/mattdurham/lth/internal/db"
	"github.com/mattdurham/lth/internal/llm"
	"github.com/mattdurham/lth/internal/vector"
)

// BackfillValence finds memories where valence_scored=false and scores them via LLM.
// It runs as a background goroutine in the daemon, processing in batches to avoid rate limits.
// Stops when ctx is canceled.
func BackfillValence(ctx context.Context, d *db.DB, llmClient llm.LLM, batchSize int, interval time.Duration) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		batch, err := d.ListUnscored(ctx, batchSize)
		if err != nil {
			slog.Warn("backfill valence: query failed", "err", err)
			backfillWait(ctx, interval)
			continue
		}

		if len(batch) == 0 {
			// Nothing to backfill; wait before polling again.
			backfillWait(ctx, interval)
			continue
		}

		slog.Info("backfilling valence", "count", len(batch))
		for _, row := range batch {
			select {
			case <-ctx.Done():
				return
			default:
			}

			resp, err := llmClient.Complete(ctx, valencePrompt(row.Content))
			if err != nil {
				slog.Warn("backfill valence: LLM error", "id", row.ID, "err", err)
				continue
			}
			v, err := parseValence(resp)
			if err != nil {
				slog.Warn("backfill valence: parse error", "id", row.ID, "resp", resp, "err", err)
				continue
			}
			if err := d.UpdateValence(context.Background(), row.ID, v); err != nil {
				slog.Warn("backfill valence: update error", "id", row.ID, "err", err)
			}
		}

		backfillWait(ctx, interval)
	}
}

// BackfillEmbeddings finds memories with null/empty embeddings and re-embeds them.
// Runs as a background goroutine in the daemon so memories stored before the embedding
// server was available become searchable without manual intervention.
func BackfillEmbeddings(ctx context.Context, d *db.DB, emb vector.Embedder, batchSize int, interval time.Duration) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		batch, err := d.ListUnembedded(ctx, batchSize)
		if err != nil {
			slog.Warn("backfill embeddings: query failed", "err", err)
			backfillWait(ctx, interval)
			continue
		}

		if len(batch) == 0 {
			backfillWait(ctx, interval)
			continue
		}

		slog.Info("backfilling embeddings", "count", len(batch))
		for _, row := range batch {
			select {
			case <-ctx.Done():
				return
			default:
			}

			embedding, err := emb.Embed(ctx, row.Content)
			if err != nil {
				slog.Warn("backfill embeddings: embed error", "id", row.ID, "err", err)
				continue
			}
			blob := vector.ToBytes(embedding)
			if err := d.UpdateEmbedding(context.Background(), row.ID, blob); err != nil {
				slog.Warn("backfill embeddings: update error", "id", row.ID, "err", err)
			}
		}

		backfillWait(ctx, interval)
	}
}

// backfillWait sleeps for d or returns early if ctx is canceled.
func backfillWait(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}
