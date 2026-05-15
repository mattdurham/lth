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

// ParseFilePaths extracts file paths from tool_use blocks in a raw JSONL line.
// Only assistant messages carry tool_use blocks. Returns nil, "" when no file
// paths are found or the line is not an assistant message.
func ParseFilePaths(line []byte) (paths []string, sessionID string) {
	var msg JSOMessage
	if err := json.Unmarshal(line, &msg); err != nil {
		return nil, ""
	}
	if msg.Type != "assistant" || msg.Message == nil {
		return nil, ""
	}
	p := ExtractFilePaths(msg.Message.Content)
	if len(p) == 0 {
		return nil, ""
	}
	return p, msg.SessionID
}

// ExtractFilePaths returns file paths from Read, Write, and Edit tool_use blocks in content.
func ExtractFilePaths(content json.RawMessage) []string {
	var blocks []struct {
		Type  string          `json:"type"`
		Name  string          `json:"name"`
		Input json.RawMessage `json:"input"`
	}
	if err := json.Unmarshal(content, &blocks); err != nil {
		return nil
	}

	var paths []string
	for _, b := range blocks {
		if b.Type != "tool_use" {
			continue
		}
		switch b.Name {
		case "Read", "Write", "Edit":
			var inp struct {
				FilePath string `json:"file_path"`
			}
			if err := json.Unmarshal(b.Input, &inp); err == nil && inp.FilePath != "" {
				paths = append(paths, inp.FilePath)
			}
		}
	}
	return paths
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
