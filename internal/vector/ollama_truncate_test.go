// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package vector

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncateUTF8(t *testing.T) {
	tests := []struct {
		name string
		in   string
		max  int
		want string
	}{
		{name: "short string unchanged", in: "hello", max: 100, want: "hello"},
		{name: "exact length unchanged", in: "hello", max: 5, want: "hello"},
		{name: "ascii truncation", in: "hello world", max: 5, want: "hello"},
		{name: "empty input", in: "", max: 10, want: ""},
		{name: "zero max", in: "abc", max: 0, want: ""},
		// 3-byte UTF-8 rune (é is 2 bytes; 中 is 3 bytes; 𝄞 is 4 bytes).
		{name: "truncate before multi-byte rune boundary",
			in:   "ab\xe4\xb8\xadcd", // "ab中cd" — 7 bytes
			max:  3,                  // would land mid-rune; should back off to 2
			want: "ab"},
		{name: "truncate at multi-byte rune end",
			in:   "ab\xe4\xb8\xadcd",
			max:  5, // exactly at end of "ab中"
			want: "ab\xe4\xb8\xad"},
		{name: "max in middle of 2-byte rune",
			in:   "a\xc3\xa9b", // "aéb" — 4 bytes
			max:  2,            // mid-é, back off to "a"
			want: "a"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := truncateUTF8(tc.in, tc.max)
			if got != tc.want {
				t.Errorf("got %q (len=%d), want %q (len=%d)", got, len(got), tc.want, len(tc.want))
			}
			if !utf8.ValidString(got) {
				t.Errorf("result is not valid UTF-8: %q", got)
			}
		})
	}
}

func TestTruncateUTF8_PreservesValidity_RandomInput(t *testing.T) {
	// A long input built of mixed-width runes; truncating at every possible
	// length must always produce valid UTF-8.
	in := strings.Repeat("a中b𝄞c", 100) // mix of 1, 3, and 4-byte runes
	for max := 0; max <= len(in); max++ {
		got := truncateUTF8(in, max)
		if !utf8.ValidString(got) {
			t.Fatalf("invalid UTF-8 at max=%d: %q", max, got)
		}
		if len(got) > max {
			t.Fatalf("result longer than max at max=%d: got len=%d", max, len(got))
		}
	}
}
