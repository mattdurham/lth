// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package patcher

import "testing"

func TestExtractPatch(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantPatch string
		wantOK    bool
	}{
		{
			name:      "happy path",
			input:     "<patch>diff --git a/foo.go b/foo.go</patch>",
			wantPatch: "diff --git a/foo.go b/foo.go",
			wantOK:    true,
		},
		{
			name:      "whitespace trimmed",
			input:     "<patch>  \n  some content  \n  </patch>",
			wantPatch: "some content",
			wantOK:    true,
		},
		{
			name:      "missing open tag",
			input:     "diff --git a/foo.go b/foo.go</patch>",
			wantPatch: "",
			wantOK:    false,
		},
		{
			name:      "missing close tag",
			input:     "<patch>diff --git a/foo.go b/foo.go",
			wantPatch: "",
			wantOK:    false,
		},
		{
			name:      "surrounding content ignored",
			input:     "Here is my fix:\n<patch>the patch</patch>\nLet me know if you need more.",
			wantPatch: "the patch",
			wantOK:    true,
		},
		{
			name:      "empty tags",
			input:     "<patch></patch>",
			wantPatch: "",
			wantOK:    true,
		},
		{
			name:      "multiple tags first wins",
			input:     "<patch>first</patch> some text <patch>second</patch>",
			wantPatch: "first",
			wantOK:    true,
		},
		{
			name: "realistic claude output",
			input: `I've analyzed the issue and here's my fix:

<patch>
--- a/pkg/foo/foo.go
+++ b/pkg/foo/foo.go
@@ -10,7 +10,7 @@
-	return nil
+	return err
</patch>

This should resolve the problem by returning the error instead of nil.`,
			wantPatch: "--- a/pkg/foo/foo.go\n+++ b/pkg/foo/foo.go\n@@ -10,7 +10,7 @@\n-\treturn nil\n+\treturn err",
			wantOK:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ExtractPatch(tt.input)
			if ok != tt.wantOK {
				t.Errorf("ok = %v, want %v", ok, tt.wantOK)
			}
			if got != tt.wantPatch {
				t.Errorf("patch = %q, want %q", got, tt.wantPatch)
			}
		})
	}
}
