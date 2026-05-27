// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package db

import (
	"context"
	"strconv"
)

// ensureVecDim checks whether the stored embedding dimension matches the configured dim.
// If not, it wipes all embeddings so BackfillEmbeddings will re-encode with the new model,
// and stores the new dimension. The vec table itself is (re)created by createSchema.
func (d *DB) ensureVecDim(dim int) error {
	ctx := context.Background()

	var stored string
	err := d.db.QueryRowContext(ctx,
		`SELECT value FROM db_metadata WHERE key = 'embed_dim'`).Scan(&stored)
	if err == nil && stored == strconv.Itoa(dim) {
		return nil // dimension unchanged
	}

	// Dimension changed or not yet recorded — wipe embeddings so they get re-encoded.
	if _, err := d.db.ExecContext(ctx,
		`UPDATE memories SET embedding = NULL, embedding_model = '' WHERE compacted_at IS NULL`); err != nil {
		return err
	}

	// Store new dimension.
	_, err = d.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO db_metadata(key, value) VALUES ('embed_dim', ?)`,
		strconv.Itoa(dim))
	return err
}
