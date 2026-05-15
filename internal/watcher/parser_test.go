// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package watcher

import (
	"strings"
	"testing"
)

func TestParseLine(t *testing.T) {
	tests := []struct {
		name        string
		line        string
		wantSkip    bool
		wantContent string
	}{
		{
			name:        "valid user message string content",
			line:        `{"type":"user","message":{"role":"user","content":"hello world"},"sessionId":"sess1","cwd":"/home/user"}`,
			wantSkip:    false,
			wantContent: "hello world",
		},
		{
			name:     "queue-operation type",
			line:     `{"type":"queue-operation","operation":"enqueue","content":"ignored"}`,
			wantSkip: true,
		},
		{
			name:     "system role",
			line:     `{"type":"user","message":{"role":"system","content":"system prompt"},"sessionId":"sess1"}`,
			wantSkip: true,
		},
		{
			name:     "no message field",
			line:     `{"type":"user","sessionId":"sess1"}`,
			wantSkip: true,
		},
		{
			name:     "empty content",
			line:     `{"type":"user","message":{"role":"user","content":""},"sessionId":"sess1"}`,
			wantSkip: true,
		},
		{
			name:        "text block array",
			line:        `{"type":"user","message":{"role":"user","content":[{"type":"text","text":"block text"}]},"sessionId":"sess1"}`,
			wantSkip:    false,
			wantContent: "block text",
		},
		{
			name:     "invalid JSON",
			line:     `{invalid json`,
			wantSkip: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			content, _, _, _, skip, _ := ParseLine([]byte(tc.line))
			if skip != tc.wantSkip {
				t.Errorf("skip = %v, want %v", skip, tc.wantSkip)
			}
			if !tc.wantSkip && content != tc.wantContent {
				t.Errorf("content = %q, want %q", content, tc.wantContent)
			}
		})
	}
}

func TestParseFilePaths(t *testing.T) {
	tests := []struct {
		name      string
		line      string
		wantPaths []string
		wantSID   string
	}{
		{
			name: "read tool_use",
			line: `{"type":"assistant","sessionId":"s1","message":{"role":"assistant","content":[{"type":"tool_use","name":"Read","input":{"file_path":"/src/foo.go"}}]}}`,
			wantPaths: []string{"/src/foo.go"},
			wantSID:   "s1",
		},
		{
			name: "write and edit tool_use",
			line: `{"type":"assistant","sessionId":"s2","message":{"role":"assistant","content":[{"type":"tool_use","name":"Write","input":{"file_path":"/src/a.go"}},{"type":"tool_use","name":"Edit","input":{"file_path":"/src/b.go"}}]}}`,
			wantPaths: []string{"/src/a.go", "/src/b.go"},
			wantSID:   "s2",
		},
		{
			name:      "bash tool_use ignored",
			line:      `{"type":"assistant","sessionId":"s3","message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash","input":{"command":"go build"}}]}}`,
			wantPaths: nil,
			wantSID:   "",
		},
		{
			name:      "user message ignored",
			line:      `{"type":"user","sessionId":"s4","message":{"role":"user","content":[{"type":"tool_use","name":"Read","input":{"file_path":"/src/foo.go"}}]}}`,
			wantPaths: nil,
			wantSID:   "",
		},
		{
			name:      "text block only",
			line:      `{"type":"assistant","sessionId":"s5","message":{"role":"assistant","content":[{"type":"text","text":"hello"}]}}`,
			wantPaths: nil,
			wantSID:   "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			paths, sid := ParseFilePaths([]byte(tc.line))
			if tc.wantPaths == nil && len(paths) != 0 {
				t.Errorf("paths = %v, want nil", paths)
			}
			if tc.wantSID != sid {
				t.Errorf("sessionID = %q, want %q", sid, tc.wantSID)
			}
			if len(tc.wantPaths) > 0 {
				if len(paths) != len(tc.wantPaths) {
					t.Fatalf("len(paths) = %d, want %d: %v", len(paths), len(tc.wantPaths), paths)
				}
				for i, p := range tc.wantPaths {
					if paths[i] != p {
						t.Errorf("paths[%d] = %q, want %q", i, paths[i], p)
					}
				}
			}
		})
	}
}

func TestExtractContent(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "plain string",
			raw:  `"hello world"`,
			want: "hello world",
		},
		{
			name: "single text block",
			raw:  `[{"type":"text","text":"block content"}]`,
			want: "block content",
		},
		{
			name: "multiple text blocks",
			raw:  `[{"type":"text","text":"first"},{"type":"text","text":"second"}]`,
			want: "first\nsecond",
		},
		{
			name: "tool_result small",
			raw:  `[{"type":"tool_result","content":"small result"}]`,
			want: "small result",
		},
		{
			name: "tool_result large",
			raw:  `[{"type":"tool_result","content":"` + strings.Repeat("x", 11000) + `"}]`,
			want: "",
		},
		{
			name: "mixed text and tool_result small",
			raw:  `[{"type":"text","text":"text part"},{"type":"tool_result","content":"small"}]`,
			want: "text part\nsmall",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ExtractContent([]byte(tc.raw))
			if got != tc.want {
				t.Errorf("ExtractContent = %q, want %q", got, tc.want)
			}
		})
	}
}
