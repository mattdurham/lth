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
