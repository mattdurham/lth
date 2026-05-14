// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package memory

import (
	"strings"
	"testing"
)

func TestParseTags(t *testing.T) {
	tests := []struct {
		name     string
		response string
		want     string
	}{
		{
			name:     "simple array",
			response: `["go", "error-handling", "nil-pointer"]`,
			want:     "go,error-handling,nil-pointer",
		},
		{
			name:     "markdown code fence",
			response: "```json\n[\"go\", \"sqlite\", \"performance\"]\n```",
			want:     "go,sqlite,performance",
		},
		{
			name:     "code fence no lang",
			response: "```\n[\"testing\", \"mocking\"]\n```",
			want:     "testing,mocking",
		},
		{
			name:     "uppercase tags get lowercased",
			response: `["Go", "Error-Handling", "SQL"]`,
			want:     "go,error-handling,sql",
		},
		{
			name:     "tags with extra whitespace",
			response: `["  go  ", " database ", "concurrency"]`,
			want:     "go,database,concurrency",
		},
		{
			name:     "more than 5 tags — truncated to 5",
			response: `["a", "b", "c", "d", "e", "f"]`,
			want:     "a,b,c,d,e",
		},
		{
			name:     "empty array",
			response: `[]`,
			want:     "",
		},
		{
			name:     "invalid JSON",
			response: `not json`,
			want:     "",
		},
		{
			name:     "empty response",
			response: "",
			want:     "",
		},
		{
			name:     "empty strings filtered out",
			response: `["go", "", "  ", "database"]`,
			want:     "go,database",
		},
		{
			name:     "single tag",
			response: `["go"]`,
			want:     "go",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseTags(tt.response)
			if got != tt.want {
				t.Errorf("parseTags(%q) = %q, want %q", tt.response, got, tt.want)
			}
		})
	}
}

func TestTagPrompt(t *testing.T) {
	content := "Go error handling patterns for nil pointer dereference"
	prompt := tagPrompt(content)

	if !strings.Contains(prompt, content) {
		t.Error("tagPrompt should include the content text")
	}
	if !strings.Contains(prompt, "JSON array") {
		t.Error("tagPrompt should mention JSON array")
	}
	if !strings.Contains(prompt, "3-5") {
		t.Error("tagPrompt should mention 3-5 tags")
	}
}
