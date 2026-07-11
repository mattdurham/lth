// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package prwatcher

import (
	"fmt"
	"strings"
)

// maxDiffChars bounds how much diff text is sent to the LLM per PR. Keeps
// each summarization call cheap regardless of how large the underlying PR
// was; a truncation note is appended so the model doesn't treat a partial
// diff as the complete change.
const maxDiffChars = 40_000

// buildPrompt assembles the LLM prompt for summarizing one merged PR.
func buildPrompt(repo, dir, title, body, author, diff string) string {
	var sb strings.Builder
	sb.WriteString("Summarize what this merged pull request changed and why, in 2-4 sentences.\n")
	sb.WriteString("Be factual and specific -- name the actual files, configs, or values changed. No fluff, no restating the title.\n\n")
	fmt.Fprintf(&sb, "Repo: %s\n", repo)
	if dir != "" {
		fmt.Fprintf(&sb, "Scope: %s\n", dir)
	}
	fmt.Fprintf(&sb, "Author: %s\n", author)
	fmt.Fprintf(&sb, "Title: %s\n\n", title)
	if strings.TrimSpace(body) != "" {
		sb.WriteString("Description:\n")
		sb.WriteString(strings.TrimSpace(body))
		sb.WriteString("\n\n")
	}
	if diff != "" {
		sb.WriteString("Diff:\n")
		sb.WriteString(diff)
	}
	return sb.String()
}

// truncateDiff caps diff to maxChars, appending a note when truncated so the
// LLM knows the diff is partial rather than assuming it saw everything.
func truncateDiff(diff string, maxChars int) string {
	if len(diff) <= maxChars {
		return diff
	}
	return diff[:maxChars] + "\n\n... [diff truncated]"
}
