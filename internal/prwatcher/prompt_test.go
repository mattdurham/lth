// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package prwatcher

import (
	"strings"
	"testing"
)

func TestBuildPromptIncludesKeyFields(t *testing.T) {
	prompt := buildPrompt("acme/widgets", "ksonnet/environments/tempo", "Bump replica count", "Because load increased.", "octocat", "diff --git a/x b/x")

	for _, want := range []string{"acme/widgets", "ksonnet/environments/tempo", "Bump replica count", "Because load increased.", "octocat", "diff --git a/x b/x"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestBuildPromptOmitsEmptyScope(t *testing.T) {
	prompt := buildPrompt("acme/widgets", "", "title", "", "octocat", "")
	if strings.Contains(prompt, "Scope:") {
		t.Errorf("prompt should omit Scope: line when dir is empty:\n%s", prompt)
	}
	if strings.Contains(prompt, "Description:") {
		t.Errorf("prompt should omit Description: section when body is empty:\n%s", prompt)
	}
	if strings.Contains(prompt, "Diff:") {
		t.Errorf("prompt should omit Diff: section when diff is empty:\n%s", prompt)
	}
}

func TestTruncateDiff(t *testing.T) {
	short := "abc"
	if got := truncateDiff(short, 10); got != short {
		t.Errorf("truncateDiff(short) = %q, want unchanged", got)
	}

	long := strings.Repeat("x", 100)
	got := truncateDiff(long, 10)
	if len(got) <= 10 {
		t.Errorf("truncateDiff should append a truncation note, got %q", got)
	}
	if !strings.HasPrefix(got, strings.Repeat("x", 10)) {
		t.Errorf("truncateDiff should preserve the first maxChars bytes, got %q", got)
	}
	if !strings.Contains(got, "truncated") {
		t.Errorf("truncateDiff should note truncation, got %q", got)
	}
}
