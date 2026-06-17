// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package gwswatcher

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mattdurham/lth/internal/config"
)

// stubRunner mocks the gws CLI. Each Run call records its args, returns the
// next queued response (FIFO), or an error if the queue is empty.
type stubRunner struct {
	calls   [][]string
	queue   []stubResponse
	pos     int
}
type stubResponse struct {
	out []byte
	err error
}

func (s *stubRunner) Run(_ context.Context, args ...string) ([]byte, error) {
	s.calls = append(s.calls, append([]string(nil), args...))
	if s.pos >= len(s.queue) {
		return nil, errors.New("stubRunner: no queued response")
	}
	r := s.queue[s.pos]
	s.pos++
	return r.out, r.err
}

func makeListResponse(files []driveFile) []byte {
	b, _ := json.Marshal(map[string]any{"files": files})
	return b
}

func makeDocResponse(title string, paragraphs []string) []byte {
	doc := docResponse{Title: title}
	for _, p := range paragraphs {
		doc.Body.Content = append(doc.Body.Content, docElement{
			Paragraph: &docParagraph{
				Elements: []docParagraphElement{{TextRun: &docTextRun{Content: p + "\n"}}},
			},
		})
	}
	b, _ := json.Marshal(doc)
	return b
}

func TestScanOnce_WritesFileAndSkipsOnSecondRun(t *testing.T) {
	tmp := t.TempDir()
	cfg := config.Default()
	cfg.GWS.Enabled = true
	cfg.GWS.OutputDir = filepath.Join(tmp, "out")
	cfg.GWS.LookbackDays = 7
	cfg.GWS.NamePatterns = []string{"Notes by Gemini"}

	modTime := "2026-06-17T16:53:09.901Z"
	file := driveFile{
		ID: "doc-1", Name: "Team Sync - Notes by Gemini",
		MimeType: "application/vnd.google-apps.document",
		ModifiedTime: modTime, WebViewLink: "https://docs/edit",
	}
	stub := &stubRunner{queue: []stubResponse{
		{out: makeListResponse([]driveFile{file})},
		{out: makeDocResponse("Team Sync - Notes by Gemini", []string{"Summary", "Decisions"})},
	}}
	w := &Watcher{cfg: cfg, runner: stub}

	if err := w.ScanOnce(context.Background()); err != nil {
		t.Fatalf("ScanOnce: %v", err)
	}

	// One .md file should now exist.
	entries, _ := os.ReadDir(cfg.GWS.OutputDir)
	if len(entries) != 1 {
		t.Fatalf("expected 1 file, got %d", len(entries))
	}
	got, _ := os.ReadFile(filepath.Join(cfg.GWS.OutputDir, entries[0].Name()))
	for _, want := range []string{
		"# Team Sync - Notes by Gemini",
		"Doc ID: doc-1",
		"Modified: " + modTime,
		"Summary",
		"Decisions",
	} {
		if !strings.Contains(string(got), want) {
			t.Errorf("output missing %q\n--- file ---\n%s", want, got)
		}
	}

	// Second cycle: same Drive metadata -> should skip the fetch (no doc call).
	stub.queue = []stubResponse{
		{out: makeListResponse([]driveFile{file})},
	}
	stub.pos = 0
	stub.calls = nil
	if err := w.ScanOnce(context.Background()); err != nil {
		t.Fatalf("second ScanOnce: %v", err)
	}
	if len(stub.calls) != 1 {
		t.Errorf("expected only the list call on second cycle (got %d total calls)", len(stub.calls))
	}
}

func TestScanOnce_RefetchesWhenDriveTimeIsNewer(t *testing.T) {
	tmp := t.TempDir()
	cfg := config.Default()
	cfg.GWS.Enabled = true
	cfg.GWS.OutputDir = filepath.Join(tmp, "out")
	cfg.GWS.LookbackDays = 7
	cfg.GWS.NamePatterns = []string{"Transcript"}

	first := driveFile{
		ID: "doc-X", Name: "Q3 Planning Transcript",
		MimeType: "application/vnd.google-apps.document",
		ModifiedTime: "2026-06-17T10:00:00Z",
	}
	stub := &stubRunner{queue: []stubResponse{
		{out: makeListResponse([]driveFile{first})},
		{out: makeDocResponse("Q3 Planning Transcript", []string{"v1 body"})},
	}}
	w := &Watcher{cfg: cfg, runner: stub}
	if err := w.ScanOnce(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Now Drive reports a newer modifiedTime -> doc must be re-fetched.
	updated := first
	updated.ModifiedTime = "2026-06-17T12:00:00Z"
	stub.queue = []stubResponse{
		{out: makeListResponse([]driveFile{updated})},
		{out: makeDocResponse("Q3 Planning Transcript", []string{"v2 body"})},
	}
	stub.pos = 0
	stub.calls = nil
	if err := w.ScanOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(stub.calls) != 2 {
		t.Errorf("expected list+fetch on second cycle (got %d calls)", len(stub.calls))
	}
	entries, _ := os.ReadDir(cfg.GWS.OutputDir)
	got, _ := os.ReadFile(filepath.Join(cfg.GWS.OutputDir, entries[0].Name()))
	if !strings.Contains(string(got), "v2 body") {
		t.Errorf("file not refreshed: %s", got)
	}
}

func TestScanOnce_FetchFailureSkipsDoc(t *testing.T) {
	tmp := t.TempDir()
	cfg := config.Default()
	cfg.GWS.Enabled = true
	cfg.GWS.OutputDir = filepath.Join(tmp, "out")
	cfg.GWS.LookbackDays = 7
	cfg.GWS.NamePatterns = []string{"Notes"}

	files := []driveFile{
		{ID: "good", Name: "Good Notes", ModifiedTime: "2026-06-17T10:00:00Z"},
		{ID: "bad", Name: "Bad Notes", ModifiedTime: "2026-06-17T11:00:00Z"},
	}
	stub := &stubRunner{queue: []stubResponse{
		{out: makeListResponse(files)},
		{out: makeDocResponse("Good Notes", []string{"ok"})},
		{err: errors.New("transient API error")},
	}}
	w := &Watcher{cfg: cfg, runner: stub}
	if err := w.ScanOnce(context.Background()); err != nil {
		t.Fatalf("ScanOnce should not fail on per-doc errors: %v", err)
	}
	entries, _ := os.ReadDir(cfg.GWS.OutputDir)
	if len(entries) != 1 {
		t.Errorf("expected 1 file (only 'good'), got %d: %v", len(entries), names(entries))
	}
}

func TestSlug(t *testing.T) {
	cases := map[string]string{
		"Team Sync - Notes by Gemini": "team-sync-notes-by-gemini",
		"Q3 Planning":                 "q3-planning",
		"  trailing space  ":          "trailing-space",
		"!!!special!!!":               "special",
	}
	for in, want := range cases {
		if got := slug(in); got != want {
			t.Errorf("slug(%q) = %q, want %q", in, got, want)
		}
	}
	if len(slug(strings.Repeat("x", 200))) != 80 {
		t.Errorf("slug should cap at 80 chars")
	}
}

func TestDocOutputPath_StableAcrossCycles(t *testing.T) {
	f := driveFile{ID: "abc123", Name: "Team Sync", ModifiedTime: "2026-06-17T10:00:00Z"}
	a := docOutputPath("/tmp", f)
	b := docOutputPath("/tmp", f)
	if a != b {
		t.Errorf("docOutputPath not deterministic: %q vs %q", a, b)
	}
	// Doc ID must appear in the path so it dedupes per-doc.
	if !strings.Contains(a, "abc123") {
		t.Errorf("path missing doc ID: %q", a)
	}
}

func TestExpandHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}
	if got := expandHome("~/foo"); got != filepath.Join(home, "foo") {
		t.Errorf("got %q", got)
	}
	if got := expandHome("/abs/path"); got != "/abs/path" {
		t.Errorf("absolute path mutated: %q", got)
	}
	if got := expandHome("rel/path"); got != "rel/path" {
		t.Errorf("relative path mutated: %q", got)
	}
}

func TestUpToDate(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "doc.md")
	_ = os.WriteFile(path, []byte("x"), 0o600)

	older := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	_ = os.Chtimes(path, older, older)

	if upToDate(path, driveFile{ModifiedTime: "2026-06-17T10:00:00Z"}) {
		t.Error("should NOT be up to date (drive is newer)")
	}
	if !upToDate(path, driveFile{ModifiedTime: "2025-06-17T10:00:00Z"}) {
		t.Error("should be up to date (drive is older)")
	}
	if upToDate(filepath.Join(tmp, "nope.md"), driveFile{ModifiedTime: "2026-06-17T10:00:00Z"}) {
		t.Error("nonexistent file is not up to date")
	}
}

func names(entries []os.DirEntry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}
