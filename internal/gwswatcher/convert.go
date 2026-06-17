// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package gwswatcher

import (
	"fmt"
	"strings"
	"time"
)

// buildDriveQuery turns include/exclude name patterns and a since-time into a
// Drive `q` expression. The result is a clause of the form:
//
//	mimeType='application/vnd.google-apps.document'
//	and modifiedTime > '<sinceRFC3339>'
//	and (name contains 'A' or name contains 'B' ...)
//	and not (name contains 'X' or name contains 'Y' ...)
//
// An empty include list yields no name-contains clause (every recently-
// modified doc qualifies). Single-quote characters in patterns are escaped
// per the Drive query language.
func buildDriveQuery(includePatterns, excludePatterns []string, since time.Time) string {
	var parts []string
	parts = append(parts, "mimeType='application/vnd.google-apps.document'")
	parts = append(parts, fmt.Sprintf("modifiedTime > '%s'", since.UTC().Format(time.RFC3339)))

	if len(includePatterns) > 0 {
		clauses := make([]string, len(includePatterns))
		for i, p := range includePatterns {
			clauses[i] = "name contains '" + escapeDriveString(p) + "'"
		}
		parts = append(parts, "("+strings.Join(clauses, " or ")+")")
	}
	if len(excludePatterns) > 0 {
		clauses := make([]string, len(excludePatterns))
		for i, p := range excludePatterns {
			clauses[i] = "name contains '" + escapeDriveString(p) + "'"
		}
		parts = append(parts, "not ("+strings.Join(clauses, " or ")+")")
	}
	return strings.Join(parts, " and ")
}

// escapeDriveString escapes single quotes and backslashes for use inside a
// Drive query string literal (see Drive API: "Search query terms").
func escapeDriveString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `'`, `\'`)
	return s
}

// docResponse mirrors the subset of the Google Docs v1 response we use.
type docResponse struct {
	Title string  `json:"title"`
	Body  docBody `json:"body"`
}
type docBody struct {
	Content []docElement `json:"content"`
}
type docElement struct {
	Paragraph    *docParagraph    `json:"paragraph,omitempty"`
	Table        *docTable        `json:"table,omitempty"`
	SectionBreak *docSectionBreak `json:"sectionBreak,omitempty"`
}
type docParagraph struct {
	Elements       []docParagraphElement `json:"elements"`
	ParagraphStyle docParagraphStyle     `json:"paragraphStyle"`
	Bullet         *docBullet            `json:"bullet,omitempty"`
}
type docParagraphElement struct {
	TextRun *docTextRun `json:"textRun,omitempty"`
}
type docTextRun struct {
	Content string `json:"content"`
}
type docParagraphStyle struct {
	NamedStyleType string `json:"namedStyleType"`
}
type docBullet struct {
	NestingLevel int `json:"nestingLevel"`
}
type docTable struct {
	TableRows []docTableRow `json:"tableRows"`
}
type docTableRow struct {
	TableCells []docTableCell `json:"tableCells"`
}
type docTableCell struct {
	Content []docElement `json:"content"`
}
type docSectionBreak struct{}

// docToMarkdown converts a Docs v1 response into a markdown string. Headings
// map to `#`/`##`/`###` etc., bullets to `-` with indentation by nesting
// level, and tables to a pipe-separated row dump. Unsupported elements (e.g.
// images, equations, embedded drawings) are skipped silently -- the goal is
// to feed text content to the markdown watcher's fact-extraction prompt, not
// to render the doc faithfully.
func docToMarkdown(doc *docResponse) string {
	var sb strings.Builder
	for _, el := range doc.Body.Content {
		writeElement(&sb, el)
	}
	return sb.String()
}

func writeElement(sb *strings.Builder, el docElement) {
	switch {
	case el.Paragraph != nil:
		writeParagraph(sb, el.Paragraph)
	case el.Table != nil:
		writeTable(sb, el.Table)
		// SectionBreak and other element kinds: skip.
	}
}

func writeParagraph(sb *strings.Builder, p *docParagraph) {
	text := paragraphText(p)
	if strings.TrimSpace(text) == "" {
		// Preserve blank-line gaps but collapse pure whitespace runs.
		sb.WriteByte('\n')
		return
	}
	prefix := paragraphPrefix(p)
	sb.WriteString(prefix)
	sb.WriteString(text)
	if !strings.HasSuffix(text, "\n") {
		sb.WriteByte('\n')
	}
}

func paragraphText(p *docParagraph) string {
	var b strings.Builder
	for _, el := range p.Elements {
		if el.TextRun != nil {
			b.WriteString(el.TextRun.Content)
		}
	}
	return b.String()
}

// paragraphPrefix returns the markdown sigil for a paragraph based on its
// namedStyleType (HEADING_1..HEADING_6 → "# ".."###### ") or bullet state
// (indented dash by nestingLevel). Returns "" for NORMAL_TEXT.
func paragraphPrefix(p *docParagraph) string {
	if p.Bullet != nil {
		return strings.Repeat("  ", p.Bullet.NestingLevel) + "- "
	}
	switch p.ParagraphStyle.NamedStyleType {
	case "TITLE", "HEADING_1":
		return "# "
	case "HEADING_2":
		return "## "
	case "HEADING_3":
		return "### "
	case "HEADING_4":
		return "#### "
	case "HEADING_5":
		return "##### "
	case "HEADING_6":
		return "###### "
	}
	return ""
}

func writeTable(sb *strings.Builder, t *docTable) {
	for _, row := range t.TableRows {
		cellTexts := make([]string, 0, len(row.TableCells))
		for _, cell := range row.TableCells {
			var inner strings.Builder
			for _, sub := range cell.Content {
				writeElement(&inner, sub)
			}
			// Flatten cell content to a single line for the pipe row.
			line := strings.ReplaceAll(strings.TrimSpace(inner.String()), "\n", " ")
			cellTexts = append(cellTexts, line)
		}
		sb.WriteString("| ")
		sb.WriteString(strings.Join(cellTexts, " | "))
		sb.WriteString(" |\n")
	}
	sb.WriteByte('\n')
}
