// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package gwswatcher

import (
	"strings"
	"testing"
	"time"
)

func TestBuildDriveQuery_IncludeOnly(t *testing.T) {
	since := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	q := buildDriveQuery([]string{"Notes by Gemini", "Transcript"}, nil, since)
	for _, want := range []string{
		"mimeType='application/vnd.google-apps.document'",
		"modifiedTime > '2026-06-10T12:00:00Z'",
		"name contains 'Notes by Gemini'",
		"name contains 'Transcript'",
		" or ",
	} {
		if !strings.Contains(q, want) {
			t.Errorf("query missing %q\nfull: %s", want, q)
		}
	}
	if strings.Contains(q, "not (") {
		t.Errorf("query should not have a 'not' clause when no exclusions: %s", q)
	}
}

func TestBuildDriveQuery_WithExclude(t *testing.T) {
	since := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	q := buildDriveQuery([]string{"Notes"}, []string{"Personal", "Draft"}, since)
	if !strings.Contains(q, "not (name contains 'Personal' or name contains 'Draft')") {
		t.Errorf("exclude clause missing: %s", q)
	}
}

func TestBuildDriveQuery_NoIncludeMatchesAllDocs(t *testing.T) {
	since := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	q := buildDriveQuery(nil, nil, since)
	// Should still constrain to docs + lookback, but no name clause.
	if strings.Contains(q, "name contains") {
		t.Errorf("empty include list should not add name clause: %s", q)
	}
}

func TestEscapeDriveString(t *testing.T) {
	if got := escapeDriveString(`O'Brien's notes`); got != `O\'Brien\'s notes` {
		t.Errorf("got %q", got)
	}
	if got := escapeDriveString(`backslash\here`); got != `backslash\\here` {
		t.Errorf("got %q", got)
	}
}

func TestDocToMarkdown_HeadingsAndBullets(t *testing.T) {
	doc := &docResponse{
		Title: "Meeting Notes",
		Body: docBody{Content: []docElement{
			{Paragraph: &docParagraph{
				ParagraphStyle: docParagraphStyle{NamedStyleType: "HEADING_1"},
				Elements:       []docParagraphElement{{TextRun: &docTextRun{Content: "Top Heading\n"}}},
			}},
			{Paragraph: &docParagraph{
				ParagraphStyle: docParagraphStyle{NamedStyleType: "NORMAL_TEXT"},
				Elements:       []docParagraphElement{{TextRun: &docTextRun{Content: "Intro paragraph\n"}}},
			}},
			{Paragraph: &docParagraph{
				ParagraphStyle: docParagraphStyle{NamedStyleType: "HEADING_2"},
				Elements:       []docParagraphElement{{TextRun: &docTextRun{Content: "Subsection\n"}}},
			}},
			{Paragraph: &docParagraph{
				Bullet:   &docBullet{NestingLevel: 0},
				Elements: []docParagraphElement{{TextRun: &docTextRun{Content: "First bullet\n"}}},
			}},
			{Paragraph: &docParagraph{
				Bullet:   &docBullet{NestingLevel: 1},
				Elements: []docParagraphElement{{TextRun: &docTextRun{Content: "Nested bullet\n"}}},
			}},
		}},
	}
	got := docToMarkdown(doc)
	for _, want := range []string{
		"# Top Heading",
		"Intro paragraph",
		"## Subsection",
		"- First bullet",
		"  - Nested bullet",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q\n--- full ---\n%s", want, got)
		}
	}
}

func TestDocToMarkdown_Table(t *testing.T) {
	doc := &docResponse{
		Body: docBody{Content: []docElement{
			{Table: &docTable{TableRows: []docTableRow{
				{TableCells: []docTableCell{
					{Content: []docElement{{Paragraph: &docParagraph{
						Elements: []docParagraphElement{{TextRun: &docTextRun{Content: "Name\n"}}},
					}}}},
					{Content: []docElement{{Paragraph: &docParagraph{
						Elements: []docParagraphElement{{TextRun: &docTextRun{Content: "Value\n"}}},
					}}}},
				}},
				{TableCells: []docTableCell{
					{Content: []docElement{{Paragraph: &docParagraph{
						Elements: []docParagraphElement{{TextRun: &docTextRun{Content: "alpha\n"}}},
					}}}},
					{Content: []docElement{{Paragraph: &docParagraph{
						Elements: []docParagraphElement{{TextRun: &docTextRun{Content: "1\n"}}},
					}}}},
				}},
			}}},
		}},
	}
	got := docToMarkdown(doc)
	if !strings.Contains(got, "| Name | Value |") {
		t.Errorf("table header missing:\n%s", got)
	}
	if !strings.Contains(got, "| alpha | 1 |") {
		t.Errorf("table row missing:\n%s", got)
	}
}

func TestDocToMarkdown_EmptyAndUnknownElements(t *testing.T) {
	doc := &docResponse{
		Body: docBody{Content: []docElement{
			{SectionBreak: &docSectionBreak{}},
			{Paragraph: &docParagraph{
				Elements: []docParagraphElement{{TextRun: &docTextRun{Content: "\n"}}},
			}},
			{Paragraph: &docParagraph{
				Elements: []docParagraphElement{{TextRun: &docTextRun{Content: "real content\n"}}},
			}},
		}},
	}
	got := docToMarkdown(doc)
	if !strings.Contains(got, "real content") {
		t.Errorf("missing content: %q", got)
	}
}
