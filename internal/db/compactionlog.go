package db

import "time"

type CompactionLog struct {
	ID          string
	RunAt       time.Time
	Path        string
	SourceLayer int
	TargetLayer int
	SourceIDs   string
	TargetID    string
}
