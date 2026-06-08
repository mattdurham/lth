// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package watcher

import (
	"encoding/json"
	"strings"
	"time"
)

// Format identifies the JSONL conversation format used by a file.
type Format int

const (
	// FormatClaude is the Claude JSONL format used in ~/.claude/projects/**/*.jsonl.
	FormatClaude Format = iota
	// FormatWllr is the wllr flat JSONL format used in ~/.wllr/sessions/**/*.jsonl.
	FormatWllr
	// FormatPi is the pi coding agent JSONL format used in ~/.pi/agent/sessions/**/*.jsonl.
	FormatPi
)

// detectFormat returns the format for a given file path.
// Paths containing "/.pi/agent/sessions/" use the pi format;
// paths containing "/.wllr/" use the wllr flat format;
// all others use the Claude format.
func detectFormat(path string) Format {
	if strings.Contains(path, "/.pi/agent/sessions/") {
		return FormatPi
	}
	if strings.Contains(path, "/.wllr/") {
		return FormatWllr
	}
	return FormatClaude
}

// wllrRecord is the raw JSON structure for a single line in a wllr JSONL file.
type wllrRecord struct {
	Type    string          `json:"type"`
	ID      string          `json:"id"`
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
	CWD     string          `json:"cwd"` // present only on session-type lines
}

// ParseWllrLine parses a single line from a wllr JSONL file.
//
// For session-type lines: returns the session cwd in the cwd return value and skip=true.
// The caller MUST update its carriedCWD state when a non-empty cwd is returned (skip=true).
//
// For message-type lines with role "user" or "assistant": returns extracted content,
// skip=false, and empty cwd. The caller should use its carriedCWD value for the memory attrs.
//
// All other lines (tool_call, system role, empty content, unknown type) return skip=true
// with empty cwd.
func ParseWllrLine(data []byte, carriedCWD string) (content, sessionID, cwd string, ts time.Time, skip bool, err error) {
	var rec wllrRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return "", "", "", ts, true, err
	}

	switch rec.Type {
	case "session":
		// Return the session cwd so the caller can carry it forward to message lines.
		// Session lines are never stored as memories.
		return "", "", rec.CWD, ts, true, nil

	case "message":
		// Skip system role.
		if rec.Role == "system" {
			return "", "", "", ts, true, nil
		}
		// Only ingest user and assistant messages.
		if rec.Role != "user" && rec.Role != "assistant" {
			return "", "", "", ts, true, nil
		}

		c := ExtractContent(rec.Content)
		if c == "" {
			return "", "", "", ts, true, nil
		}

		// sessionID is not present per-message in wllr format.
		// cwd is empty; the caller uses its carriedCWD for memory attrs.
		return c, "", "", ts, false, nil

	default:
		// tool_call and all other types are skipped.
		return "", "", "", ts, true, nil
	}
}
