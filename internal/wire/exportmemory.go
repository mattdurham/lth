package wire

import "time"

type ExportMemory struct {
	ID             string            `json:"id"`
	Layer          int               `json:"layer"`
	Content        string            `json:"content"`
	ContentHash    string            `json:"content_hash"`
	Embedding      []float32         `json:"embedding,omitempty"`
	Importance     float32           `json:"importance"`
	AccessCount    int               `json:"access_count"`
	CreatedAt      time.Time         `json:"created_at"`
	UpdatedAt      time.Time         `json:"updated_at"`
	LastAccessedAt time.Time         `json:"last_accessed_at"`
	DecayRate      float32           `json:"decay_rate"`
	Stability      float32           `json:"stability"`
	Source         string            `json:"source,omitempty"`
	Agent          string            `json:"agent,omitempty"`
	Valence        float32           `json:"valence"`
	ValenceScored  bool              `json:"valence_scored"`
	Attrs          map[string]string `json:"attrs,omitempty"`
	EmbeddingModel string            `json:"embedding_model,omitempty"`
}
