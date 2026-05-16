// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package patcher

import "strings"

// ExtractPatch finds the content between the first <patch> and </patch> tags in output.
// Returns ("", false) if either tag is missing.
func ExtractPatch(output string) (patch string, ok bool) {
	const open, close = "<patch>", "</patch>"
	start := strings.Index(output, open)
	if start == -1 {
		return "", false
	}
	start += len(open)
	end := strings.Index(output[start:], close)
	if end == -1 {
		return "", false
	}
	return strings.TrimSpace(output[start : start+end]), true
}
