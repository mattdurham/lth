package main

import "time"

// exportEdge is the JSON wire format for an edge inside an `lth export` zip.
//
// The synthetic `id` field was removed when memory_edges adopted the natural
// composite primary key (from_id, to_id, edge_type). For backward compatibility
// with older exports that include an "id" field, json.Unmarshal silently ignores
// unknown fields by default — old archives still import cleanly.
type exportEdge struct {
	FromID    string    `json:"from_id"`
	ToID      string    `json:"to_id"`
	EdgeType  string    `json:"edge_type"`
	Weight    float32   `json:"weight"`
	CreatedAt time.Time `json:"created_at"`
}
