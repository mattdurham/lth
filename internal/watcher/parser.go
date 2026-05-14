// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package watcher

import (
	"encoding/json"
	"strings"
	"time"
)

// JSOMessage represents a single line from a Claude JSONL conversation file.
type JSOMessage struct {
	Type      string          `json:"type"`
	Timestamp time.Time       `json:"timestamp"`
	SessionID string          `json:"sessionId"`
	UUID      string          `json:"uuid"`
	CWD       string          `json:"cwd"`
	Message   *JSOMessageBody `json:"message"`
}

// ParseLine parses a single JSONL line and returns the extracted content.
// Returns skip=true for lines that should not be ingested.
func ParseLine(line []byte) (content, sessionID, cwd string, ts time.Time, skip bool, err error) {
	var msg JSOMessage
	if err := json.Unmarshal(line, &msg); err != nil {
		return "", "", "", ts, true, err
	}

	// Only ingest user and assistant messages.
	if msg.Type != "user" && msg.Type != "assistant" {
		return "", "", "", ts, true, nil
	}

	if msg.Message == nil {
		return "", "", "", ts, true, nil
	}

	// Skip system role.
	if msg.Message.Role == "system" {
		return "", "", "", ts, true, nil
	}

	content = ExtractContent(msg.Message.Content)
	if content == "" {
		return "", "", "", ts, true, nil
	}

	return content, msg.SessionID, msg.CWD, msg.Timestamp, false, nil
}

// ExtractContent extracts text content from a JSON content field.
// The content can be a plain string or an array of content blocks.
// Tool result blocks larger than 10KB are skipped.
func ExtractContent(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}

	// Try plain string first.
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}

	// Try array of content blocks.
	var blocks []struct {
		Type    string `json:"type"`
		Text    string `json:"text"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return ""
	}

	var parts []string
	for _, b := range blocks {
		switch b.Type {
		case "text":
			if b.Text != "" {
				parts = append(parts, b.Text)
			}
		case "tool_result":
			if len(b.Content) <= 10*1024 {
				parts = append(parts, b.Content)
			}
			// Skip tool_result blocks > 10KB.
		}
	}

	return strings.Join(parts, "\n")
}
