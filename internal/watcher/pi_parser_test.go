// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package watcher

import (
	"reflect"
	"testing"
)

func TestDetectFormatPi(t *testing.T) {
	tests := []struct {
		name string
		path string
		want Format
	}{
		{
			name: "pi sessions path",
			path: "/home/user/.pi/agent/sessions/--home-user-proj--/x.jsonl",
			want: FormatPi,
		},
		{
			name: "pi nested sessions path",
			path: "/home/user/.pi/agent/sessions/abc/2026/x.jsonl",
			want: FormatPi,
		},
		{
			name: "wllr path still wllr",
			path: "/home/user/.wllr/sessions/abc/foo.jsonl",
			want: FormatWllr,
		},
		{
			name: "pi root (no sessions subdir) is NOT pi",
			path: "/home/user/.pi/agent/skills/foo.jsonl",
			want: FormatClaude,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := detectFormat(tc.path); got != tc.want {
				t.Errorf("detectFormat(%q) = %v, want %v", tc.path, got, tc.want)
			}
		})
	}
}

func TestParsePiLine(t *testing.T) {
	tests := []struct {
		name          string
		line          string
		wantSkip      bool
		wantContent   string
		wantCWD       string // only non-empty for session lines
		wantSessionID string // only non-empty for session lines
		wantErr       bool
	}{
		{
			name:          "session header carries id and cwd",
			line:          `{"type":"session","version":3,"id":"019ea766","timestamp":"2026-06-08T13:23:24.502Z","cwd":"/home/u/proj"}`,
			wantSkip:      true,
			wantSessionID: "019ea766",
			wantCWD:       "/home/u/proj",
		},
		{
			name:        "user text block message",
			line:        `{"type":"message","id":"m1","message":{"role":"user","content":[{"type":"text","text":"hello"}]}}`,
			wantSkip:    false,
			wantContent: "hello",
		},
		{
			name:        "assistant text + thinking — thinking dropped",
			line:        `{"type":"message","id":"m2","message":{"role":"assistant","content":[{"type":"thinking","thinking":"hmm"},{"type":"text","text":"answer"}]}}`,
			wantSkip:    false,
			wantContent: "answer",
		},
		{
			name:        "assistant toolCall only — no text content, skipped",
			line:        `{"type":"message","id":"m3","message":{"role":"assistant","content":[{"type":"toolCall","id":"t1","name":"read","arguments":{"path":"/x"}}]}}`,
			wantSkip:    true,
		},
		{
			name:        "toolResult role with text",
			line:        `{"type":"message","id":"m4","message":{"role":"toolResult","content":[{"type":"text","text":"file contents"}]}}`,
			wantSkip:    false,
			wantContent: "file contents",
		},
		{
			name:     "system role skipped",
			line:     `{"type":"message","id":"m5","message":{"role":"system","content":[{"type":"text","text":"sys"}]}}`,
			wantSkip: true,
		},
		{
			name:     "model_change skipped",
			line:     `{"type":"model_change","id":"x","modelId":"claude-opus-4-7"}`,
			wantSkip: true,
		},
		{
			name:     "thinking_level_change skipped",
			line:     `{"type":"thinking_level_change","thinkingLevel":"medium"}`,
			wantSkip: true,
		},
		{
			name:     "empty content array skipped",
			line:     `{"type":"message","id":"m6","message":{"role":"user","content":[]}}`,
			wantSkip: true,
		},
		{
			name:     "invalid JSON returns error",
			line:     `{not json`,
			wantSkip: true,
			wantErr:  true,
		},
		{
			name:        "plain string content (defensive)",
			line:        `{"type":"message","id":"m7","message":{"role":"user","content":"plain"}}`,
			wantSkip:    false,
			wantContent: "plain",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			content, sessionID, cwd, _, skip, err := ParsePiLine([]byte(tc.line))
			if tc.wantErr && err == nil {
				t.Errorf("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if skip != tc.wantSkip {
				t.Fatalf("skip = %v, want %v", skip, tc.wantSkip)
			}
			if !tc.wantSkip && content != tc.wantContent {
				t.Errorf("content = %q, want %q", content, tc.wantContent)
			}
			if tc.wantCWD != "" && cwd != tc.wantCWD {
				t.Errorf("cwd = %q, want %q", cwd, tc.wantCWD)
			}
			if tc.wantSessionID != "" && sessionID != tc.wantSessionID {
				t.Errorf("sessionID = %q, want %q", sessionID, tc.wantSessionID)
			}
		})
	}
}

func TestExtractPiFilePaths(t *testing.T) {
	tests := []struct {
		name string
		line string
		want []string
	}{
		{
			name: "read tool call",
			line: `{"type":"message","message":{"role":"assistant","content":[{"type":"toolCall","name":"read","arguments":{"path":"/a/b.go"}}]}}`,
			want: []string{"/a/b.go"},
		},
		{
			name: "write + edit + bash (bash ignored)",
			line: `{"type":"message","message":{"role":"assistant","content":[{"type":"toolCall","name":"write","arguments":{"path":"/x"}},{"type":"toolCall","name":"edit","arguments":{"path":"/y"}},{"type":"toolCall","name":"bash","arguments":{"command":"ls"}}]}}`,
			want: []string{"/x", "/y"},
		},
		{
			name: "user role yields nothing",
			line: `{"type":"message","message":{"role":"user","content":[{"type":"toolCall","name":"read","arguments":{"path":"/a"}}]}}`,
			want: nil,
		},
		{
			name: "session line yields nothing",
			line: `{"type":"session","id":"x","cwd":"/y"}`,
			want: nil,
		},
		{
			name: "missing path arg",
			line: `{"type":"message","message":{"role":"assistant","content":[{"type":"toolCall","name":"read","arguments":{}}]}}`,
			want: nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ExtractPiFilePaths([]byte(tc.line))
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}
