// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package watcher

import "encoding/json"

// JSOMessageBody is the nested message object within a JSOMessage.
type JSOMessageBody struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}
