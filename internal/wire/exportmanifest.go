package wire

import "time"

type ExportManifest struct {
	ExportedAt  time.Time `json:"exported_at"`
	ChunkSize   int       `json:"chunk_size"`
	MemoryCount int       `json:"memory_count"`
	EdgeCount   int       `json:"edge_count"`
	Files       []string  `json:"files"`
}
