// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package parquet

import "time"

// MemoryRecord is the Parquet schema struct for a synced memory.
// Embedding is stored as raw IEEE 754 little-endian float32 bytes (BYTE_ARRAY).
type MemoryRecord struct {
	ID             string    `parquet:"id,zstd"`
	Layer          int32     `parquet:"layer"`
	Content        string    `parquet:"content,zstd"`
	ContentHash    string    `parquet:"content_hash,zstd"`
	Embedding      []byte    `parquet:"embedding,zstd"`
	Importance     float32   `parquet:"importance"`
	AccessCount    int32     `parquet:"access_count"`
	CreatedAt      time.Time `parquet:"created_at"`
	UpdatedAt      time.Time `parquet:"updated_at"`
	LastAccessedAt time.Time `parquet:"last_accessed_at"`
	DecayRate      float32   `parquet:"decay_rate"`
	Stability      float32   `parquet:"stability"`
	Source         string    `parquet:"source,zstd"`
	Agent          string    `parquet:"agent,zstd"`
	Valence        float32   `parquet:"valence"`
	ValenceScored  bool      `parquet:"valence_scored"`
	EmbeddingModel string    `parquet:"embedding_model,zstd"`
}
