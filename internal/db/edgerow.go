package db

import "time"

type EdgeRow struct {
	ID        string
	FromID    string
	ToID      string
	EdgeType  string
	Weight    float32
	CreatedAt time.Time
}
