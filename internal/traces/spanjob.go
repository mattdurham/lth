// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package traces

import "time"

type spanJob struct {
	span       Span
	receivedAt time.Time
}
