// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

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
