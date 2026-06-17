// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package mdwatcher

import (
	"strings"
	"testing"
)

func TestSplitByHeading_PreservesLeadingChunk(t *testing.T) {
	in := "intro paragraph\n\n# First\nbody\n# Second\nmore\n"
	chunks := splitByHeading(in)
	if len(chunks) != 3 {
		t.Fatalf("got %d chunks, want 3", len(chunks))
	}
	if !strings.HasPrefix(chunks[0], "intro") {
		t.Errorf("chunk[0] = %q", chunks[0])
	}
	if !strings.HasPrefix(chunks[1], "# First") {
		t.Errorf("chunk[1] = %q", chunks[1])
	}
}

func TestSplitByYAMLDocs(t *testing.T) {
	in := "kind: A\nname: a\n---\nkind: B\nname: b\n---\nkind: C\n"
	chunks := splitByYAMLDocs(in)
	if len(chunks) != 3 {
		t.Fatalf("got %d chunks, want 3: %v", len(chunks), chunks)
	}
	for i, want := range []string{"kind: A", "kind: B", "kind: C"} {
		if !strings.Contains(chunks[i], want) {
			t.Errorf("chunk[%d] missing %q: %q", i, want, chunks[i])
		}
	}
	// Separators themselves should not appear in any chunk.
	for i, c := range chunks {
		for _, line := range strings.Split(c, "\n") {
			if strings.TrimRight(line, " \t") == "---" {
				t.Errorf("chunk[%d] still contains a --- separator: %q", i, c)
			}
		}
	}
}

func TestSplitByYAMLDocs_NoSeparator(t *testing.T) {
	in := "kind: Foo\nname: bar\n"
	chunks := splitByYAMLDocs(in)
	if len(chunks) != 1 || chunks[0] != in {
		t.Errorf("expected single passthrough chunk, got %v", chunks)
	}
}

func TestWindowByLines_RespectsMaxBytes(t *testing.T) {
	// 20 lines of "abcdefghi" (10 bytes including newline) = 200 bytes total
	var b strings.Builder
	for range 20 {
		b.WriteString("abcdefghi\n")
	}
	chunks := windowByLines(b.String(), 50)
	for i, c := range chunks {
		if len(c) > 50 {
			t.Errorf("chunk[%d] size %d > 50", i, len(c))
		}
	}
	// Reassembled content should equal the input.
	rejoined := strings.Join(chunks, "")
	if rejoined != b.String() {
		t.Errorf("windowByLines lost content (got %d bytes, want %d)", len(rejoined), b.Len())
	}
}

func TestWindowByLines_GiantLinePassesThrough(t *testing.T) {
	// A single 200-byte line exceeds the 50-byte cap. We expect it to be
	// emitted as its own oversized chunk rather than truncated.
	huge := strings.Repeat("X", 200)
	chunks := windowByLines(huge+"\n", 50)
	if len(chunks) != 1 {
		t.Fatalf("got %d chunks, want 1", len(chunks))
	}
	if !strings.HasPrefix(chunks[0], huge) {
		t.Errorf("oversized line was truncated")
	}
}

func TestSplitForLLM_Dispatch(t *testing.T) {
	const big = 200
	// Markdown with two H1s -> heading split
	mdContent := strings.Repeat("a", 150) + "\n# Heading\n" + strings.Repeat("b", 150) + "\n"
	mdChunks := splitForLLM("/p/x.md", mdContent, big)
	if len(mdChunks) < 2 {
		t.Errorf("md should split on headings, got %d chunks", len(mdChunks))
	}

	// YAML with two docs -> doc split
	yamlContent := strings.Repeat("a", 150) + "\n---\n" + strings.Repeat("b", 150) + "\n"
	yamlChunks := splitForLLM("/p/x.yaml", yamlContent, big)
	if len(yamlChunks) < 2 {
		t.Errorf("yaml should split on ---, got %d chunks", len(yamlChunks))
	}

	// JSON with no natural separator -> size-windowed
	var sb strings.Builder
	for range 100 {
		sb.WriteString("line of text in a json\n")
	}
	jsonChunks := splitForLLM("/p/x.json", sb.String(), big)
	if len(jsonChunks) < 2 {
		t.Errorf("json should size-window, got %d chunks of total %d bytes", len(jsonChunks), sb.Len())
	}
	for i, c := range jsonChunks {
		if len(c) > big {
			t.Errorf("json chunk[%d] size %d > %d", i, len(c), big)
		}
	}

	// Jsonnet falls into the size-windowed default path
	jsonnetChunks := splitForLLM("/p/x.jsonnet", sb.String(), big)
	if len(jsonnetChunks) < 2 {
		t.Errorf("jsonnet should size-window, got %d chunks", len(jsonnetChunks))
	}
}

func TestSplitForLLM_OversizedHeadingChunkGetsWindowed(t *testing.T) {
	// A markdown file with one H1 followed by an enormous body. The heading
	// split returns one chunk; that single chunk should then be size-windowed.
	body := strings.Repeat("line of content\n", 500) // ~8KB
	in := "# Big Section\n" + body
	chunks := splitForLLM("/p/x.md", in, 1000)
	if len(chunks) < 2 {
		t.Fatalf("oversized markdown chunk should be further windowed, got %d chunks", len(chunks))
	}
	for i, c := range chunks {
		// Allow one giant line through but section boundaries should mostly stay under cap.
		if len(c) > 1200 {
			t.Errorf("chunk[%d] not windowed: size %d", i, len(c))
		}
	}
}
