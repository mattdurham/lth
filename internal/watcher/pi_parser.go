// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package watcher

import (
	"encoding/json"
	"strings"
	"time"
)

// piRecord is the raw JSON structure for a single line in a pi agent JSONL file.
//
// Pi session log lines come in two relevant top-level shapes:
//
//  1. Session header (one per file, always the first non-empty line):
//     {"type":"session","version":3,"id":"<uuid>","timestamp":"...","cwd":"..."}
//
//  2. Message records:
//     {"type":"message","id":"...","parentId":"...","timestamp":"...",
//      "message":{"role":"user|assistant|toolResult",
//                 "content":[<blocks>],
//                 "timestamp":<unix-ms>}}
//
// Other top-level types (model_change, thinking_level_change, etc.) are ignored.
type piRecord struct {
	Type    string `json:"type"`
	ID      string `json:"id"`
	CWD     string `json:"cwd"` // present only on session-type lines
	Message *struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

// piBlock is a single content block inside a pi message.
//
// Three block types matter for ingestion:
//   - "text"      → `text` field contains plain text content
//   - "thinking"  → `thinking` field contains assistant internal reasoning (skipped)
//   - "toolCall"  → assistant tool invocation; `name` + `arguments` map (handled by ExtractPiFilePaths)
type piBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	Thinking  string          `json:"thinking"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// ParsePiLine parses a single line from a pi agent JSONL file.
//
// For session-type lines: returns the session cwd and sessionID and skip=true.
// The caller MUST update its carried state (cwd + sessionID) from these return values.
//
// For message-type lines with role "user", "assistant", or "toolResult": returns
// extracted text content, skip=false, and empty cwd/sessionID. The caller uses
// its carried sessionID and carried cwd for memory attrs.
//
// All other lines (model_change, thinking_level_change, system role, empty content,
// unknown type) return skip=true with empty cwd and sessionID.
//
// Pi format note: the session header carries BOTH the sessionID and cwd; subsequent
// message lines do not repeat them. This differs from Claude (every line carries them)
// and matches wllr (header carries cwd) plus extends it (header also carries sessionID).
func ParsePiLine(data []byte) (content, sessionID, cwd string, ts time.Time, skip bool, err error) {
	var rec piRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return "", "", "", ts, true, err
	}

	switch rec.Type {
	case "session":
		// Carry both the sessionID and cwd from the header forward.
		// Session lines are never stored as memories.
		return "", rec.ID, rec.CWD, ts, true, nil

	case "message":
		if rec.Message == nil {
			return "", "", "", ts, true, nil
		}
		role := rec.Message.Role
		// Accept user, assistant, and toolResult (analog of Claude's tool_result blocks).
		// Skip system and any other roles.
		if role != "user" && role != "assistant" && role != "toolResult" {
			return "", "", "", ts, true, nil
		}

		c := extractPiContent(rec.Message.Content)
		if c == "" {
			return "", "", "", ts, true, nil
		}
		return c, "", "", ts, false, nil

	default:
		// model_change, thinking_level_change, and unknown types are skipped.
		return "", "", "", ts, true, nil
	}
}

// extractPiContent returns concatenated text from pi message content blocks.
//
// Blocks are filtered as follows:
//   - "text"      → included as-is
//   - "thinking"  → skipped (assistant internal reasoning; not memory-worthy)
//   - "toolCall"  → skipped (tool invocations are captured separately via ExtractPiFilePaths)
//   - other types → skipped
//
// If content is a plain JSON string (not an array of blocks), it is returned directly.
// Tool result text blocks larger than 10KB are skipped to mirror Claude's tool_result cap.
func extractPiContent(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}

	// Try plain string first.
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}

	var blocks []piBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return ""
	}

	var parts []string
	for _, b := range blocks {
		switch b.Type {
		case "text":
			if b.Text == "" {
				continue
			}
			// Cap any single text block at 10KB to bound tool-result size,
			// mirroring the Claude tool_result invariant.
			if len(b.Text) > 10*1024 {
				continue
			}
			parts = append(parts, b.Text)
			// "thinking" and "toolCall" intentionally omitted.
		}
	}

	return strings.Join(parts, "\n")
}

// ExtractPiFilePaths extracts file paths from toolCall blocks in a raw pi JSONL line.
//
// Only assistant messages carry toolCall blocks. Returns nil when the line is not an
// assistant message or contains no Read/Write/Edit tool calls.
//
// Pi tool names are lowercase (`read`, `write`, `edit`) and the file path argument
// key is `path` (not `file_path` as in Claude).
func ExtractPiFilePaths(data []byte) []string {
	var rec piRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil
	}
	if rec.Type != "message" || rec.Message == nil || rec.Message.Role != "assistant" {
		return nil
	}

	var blocks []piBlock
	if err := json.Unmarshal(rec.Message.Content, &blocks); err != nil {
		return nil
	}

	var paths []string
	for _, b := range blocks {
		if b.Type != "toolCall" {
			continue
		}
		switch b.Name {
		case "read", "write", "edit":
			var args struct {
				Path string `json:"path"`
			}
			if err := json.Unmarshal(b.Arguments, &args); err == nil && args.Path != "" {
				paths = append(paths, args.Path)
			}
		}
	}
	return paths
}
