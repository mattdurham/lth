package watcher

import "encoding/json"

type JSOMessageBody struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}
