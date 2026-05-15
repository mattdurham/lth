// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package db

import "time"

// CompactionLog is a flat struct matching the columns of the compaction_log table.
type CompactionLog struct {
	ID          string
	RunAt       time.Time
	Path        string
	SourceLayer int
	TargetLayer int
	SourceIDs   string // comma-separated list of source memory IDs
	TargetID    string
}
