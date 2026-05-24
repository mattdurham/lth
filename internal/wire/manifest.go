// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package wire

import "time"

// ExportManifest is the wire format struct for the manifest.json in a ZIP+JSONL archive.
type ExportManifest struct {
	ExportedAt  time.Time `json:"exported_at"`
	ChunkSize   int       `json:"chunk_size"`
	MemoryCount int       `json:"memory_count"`
	EdgeCount   int       `json:"edge_count"`
	Files       []string  `json:"files"`
}
