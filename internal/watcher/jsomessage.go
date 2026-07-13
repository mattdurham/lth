// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package watcher

import "time"

type JSOMessage struct {
	Type      string          `json:"type"`
	Timestamp time.Time       `json:"timestamp"`
	SessionID string          `json:"sessionId"`
	UUID      string          `json:"uuid"`
	CWD       string          `json:"cwd"`
	Message   *JSOMessageBody `json:"message"`
}
