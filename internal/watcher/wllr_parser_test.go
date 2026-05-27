// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package watcher

import (
	"testing"
)

func TestDetectFormat(t *testing.T) {
	tests := []struct {
		name string
		path string
		want Format
	}{
		{
			name: "wllr sessions path",
			path: "/home/user/.wllr/sessions/abc/foo.jsonl",
			want: FormatWllr,
		},
		{
			name: "wllr root path — also matches /.wllr/",
			path: "/home/user/.wllr/foo.jsonl",
			want: FormatWllr, // contains "/.wllr/" segment
		},
		{
			name: "wllr with subdirectory",
			path: "/home/user/.wllr/sessions/2024/session.jsonl",
			want: FormatWllr,
		},
		{
			name: "claude projects path",
			path: "/home/user/.claude/projects/abc/foo.jsonl",
			want: FormatClaude,
		},
		{
			name: "other path",
			path: "/home/user/Documents/notes.jsonl",
			want: FormatClaude,
		},
		{
			name: "empty path",
			path: "",
			want: FormatClaude,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := detectFormat(tc.path)
			if got != tc.want {
				t.Errorf("detectFormat(%q) = %v, want %v", tc.path, got, tc.want)
			}
		})
	}
}

func TestParseWllrLine(t *testing.T) {
	// Note: ParseWllrLine returns the cwd return value ONLY for session-type lines.
	// For message lines, cwd is always "" — the caller is responsible for passing
	// carriedCWD from the session line into memory attrs directly.
	tests := []struct {
		name          string
		line          string
		carriedCWD    string
		wantSkip      bool
		wantContent   string
		wantCWD       string // only non-empty for session lines
		wantSessionID string
		wantErr       bool
	}{
		{
			name:        "session line carries cwd",
			line:        `{"type":"session","id":"abc","cwd":"/home/user/project"}`,
			carriedCWD:  "",
			wantSkip:    true,
			wantCWD:     "/home/user/project",
			wantContent: "",
		},
		{
			name:        "session line with empty cwd",
			line:        `{"type":"session","id":"abc","cwd":""}`,
			carriedCWD:  "",
			wantSkip:    true,
			wantCWD:     "",
			wantContent: "",
		},
		{
			name:        "user message string content",
			line:        `{"type":"message","id":"m1","role":"user","content":"hello"}`,
			carriedCWD:  "/home/user/project",
			wantSkip:    false,
			wantContent: "hello",
			wantCWD:     "", // ParseWllrLine returns empty cwd for messages; caller uses carriedCWD
		},
		{
			name:        "assistant message string content",
			line:        `{"type":"message","id":"m2","role":"assistant","content":"hi there"}`,
			carriedCWD:  "/home/user/project",
			wantSkip:    false,
			wantContent: "hi there",
			wantCWD:     "", // ParseWllrLine returns empty cwd for messages; caller uses carriedCWD
		},
		{
			name:       "system role is skipped",
			line:       `{"type":"message","id":"m3","role":"system","content":"system prompt"}`,
			carriedCWD: "/home/user/project",
			wantSkip:   true,
		},
		{
			name:       "unknown role is skipped",
			line:       `{"type":"message","id":"m4","role":"tool","content":"tool output"}`,
			carriedCWD: "/home/user/project",
			wantSkip:   true,
		},
		{
			name:       "empty content is skipped",
			line:       `{"type":"message","id":"m5","role":"user","content":""}`,
			carriedCWD: "/home/user/project",
			wantSkip:   true,
		},
		{
			name:       "tool_call type is skipped",
			line:       `{"type":"tool_call","id":"t1","tool_name":"exec","input":{"command":"go build"}}`,
			carriedCWD: "",
			wantSkip:   true,
		},
		{
			name:       "unknown type is skipped",
			line:       `{"type":"thinking","id":"x1","content":"internal reasoning"}`,
			carriedCWD: "",
			wantSkip:   true,
		},
		{
			name:     "invalid JSON returns error",
			line:     `{invalid json`,
			wantSkip: true,
			wantErr:  true,
		},
		{
			name:        "user message with text block array content",
			line:        `{"type":"message","id":"m6","role":"user","content":[{"type":"text","text":"block text"}]}`,
			carriedCWD:  "/home/user/project",
			wantSkip:    false,
			wantContent: "block text",
			wantCWD:     "", // ParseWllrLine returns empty cwd for messages; caller uses carriedCWD
		},
		{
			name:          "message returns empty sessionID",
			line:          `{"type":"message","id":"m7","role":"user","content":"test"}`,
			carriedCWD:    "",
			wantSkip:      false,
			wantContent:   "test",
			wantSessionID: "",
		},
		{
			name:        "message cwd field on line is ignored — caller uses carriedCWD",
			line:        `{"type":"message","id":"m8","role":"user","content":"check cwd","cwd":"/should/be/ignored"}`,
			carriedCWD:  "/carried/path",
			wantSkip:    false,
			wantContent: "check cwd",
			wantCWD:     "", // ParseWllrLine always returns empty cwd for messages
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			content, sessionID, cwd, _, skip, err := ParseWllrLine([]byte(tc.line), tc.carriedCWD)

			if tc.wantErr && err == nil {
				t.Errorf("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if skip != tc.wantSkip {
				t.Errorf("skip = %v, want %v", skip, tc.wantSkip)
			}
			if !tc.wantSkip {
				if content != tc.wantContent {
					t.Errorf("content = %q, want %q", content, tc.wantContent)
				}
				if cwd != tc.wantCWD {
					t.Errorf("cwd = %q, want %q", cwd, tc.wantCWD)
				}
				if sessionID != tc.wantSessionID {
					t.Errorf("sessionID = %q, want %q", sessionID, tc.wantSessionID)
				}
			}
			// For session lines: verify we get the cwd even though skip=true.
			if tc.wantSkip && tc.wantCWD != "" {
				if cwd != tc.wantCWD {
					t.Errorf("session cwd = %q, want %q", cwd, tc.wantCWD)
				}
			}
		})
	}
}

func TestParseWllrLineCarriedCWD(t *testing.T) {
	// Simulate the real caller pattern: parse session line first to get cwd,
	// then pass it as carriedCWD to subsequent message lines.
	// The caller (ingestFile) uses carriedCWD directly for memory attrs —
	// ParseWllrLine does NOT return cwd for message lines.

	sessionLine := []byte(`{"type":"session","id":"sess-1","cwd":"/home/user/myproject"}`)
	msgLine := []byte(`{"type":"message","id":"m1","role":"user","content":"what does lth do?"}`)

	// Parse the session line — should give us the cwd and skip=true.
	_, _, sessionCWD, _, skip, err := ParseWllrLine(sessionLine, "")
	if err != nil {
		t.Fatalf("parsing session line: %v", err)
	}
	if !skip {
		t.Fatalf("session line: expected skip=true, got false")
	}
	if sessionCWD != "/home/user/myproject" {
		t.Fatalf("session cwd = %q, want %q", sessionCWD, "/home/user/myproject")
	}

	// Parse the message line — pass the session cwd as carriedCWD.
	// ParseWllrLine returns empty cwd for message lines.
	// The caller (ingestFile) uses sessionCWD directly for the attrs["cwd"] field.
	content, sessionID, msgCWD, _, skip, err := ParseWllrLine(msgLine, sessionCWD)
	if err != nil {
		t.Fatalf("parsing message line: %v", err)
	}
	if skip {
		t.Fatalf("message line: expected skip=false, got true")
	}
	if content != "what does lth do?" {
		t.Errorf("content = %q, want %q", content, "what does lth do?")
	}
	// ParseWllrLine returns empty cwd for message lines — caller uses carriedCWD directly.
	if msgCWD != "" {
		t.Errorf("ParseWllrLine message cwd = %q, want empty (caller uses carriedCWD directly)", msgCWD)
	}
	// The carried cwd (used by ingestFile for memory attrs) is sessionCWD.
	if sessionCWD != "/home/user/myproject" {
		t.Errorf("carriedCWD = %q, want %q", sessionCWD, "/home/user/myproject")
	}
	if sessionID != "" {
		t.Errorf("sessionID = %q, want empty string", sessionID)
	}
}
