package main

import "time"

type exportEdge struct {
	ID        string    `json:"id"`
	FromID    string    `json:"from_id"`
	ToID      string    `json:"to_id"`
	EdgeType  string    `json:"edge_type"`
	Weight    float32   `json:"weight"`
	CreatedAt time.Time `json:"created_at"`
}
