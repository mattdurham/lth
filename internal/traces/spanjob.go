package traces

import "time"

type spanJob struct {
	span       Span
	receivedAt time.Time
}
