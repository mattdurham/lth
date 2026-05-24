package main

import "time"

type exportMetadata struct {
	LTHVersion  string         `json:"lth_version"`
	ExportedAt  time.Time      `json:"exported_at"`
	MemoryCount int            `json:"memory_count"`
	EdgeCount   int            `json:"edge_count"`
	ChunkSize   int            `json:"chunk_size"`
	LayerCounts map[string]int `json:"layer_counts"`
}
