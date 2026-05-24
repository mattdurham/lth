// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package wire

import "time"

// ExportMetadata is the wire format struct for the metadata.json in a ZIP+JSONL archive.
type ExportMetadata struct {
	LTHVersion  string         `json:"lth_version"`
	ExportedAt  time.Time      `json:"exported_at"`
	MemoryCount int            `json:"memory_count"`
	EdgeCount   int            `json:"edge_count"`
	ChunkSize   int            `json:"chunk_size"`
	LayerCounts map[string]int `json:"layer_counts"`
}
