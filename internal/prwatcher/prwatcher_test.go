// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package prwatcher

import (
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestIsSkippedAuthor(t *testing.T) {
	skip := []string{"renovate[bot]", "dependabot[bot]"}

	cases := map[string]bool{
		"renovate[bot]":   true,
		"RENOVATE[BOT]":   true, // case-insensitive
		"dependabot[bot]": true,
		"octocat":         false,
		"":                false,
	}
	for login, want := range cases {
		if got := isSkippedAuthor(login, skip); got != want {
			t.Errorf("isSkippedAuthor(%q) = %v, want %v", login, got, want)
		}
	}
}

func TestMarkSeenInitializesNilMap(t *testing.T) {
	var rs sourceState
	markSeen(&rs, "abc123")
	if !rs.SeenCommits["abc123"] {
		t.Errorf("markSeen did not record the commit")
	}
}

func TestRecordOutcomeStoredVsSkipped(t *testing.T) {
	var rs sourceState

	recordOutcome(&rs, 42, prOutcome{Stored: true, Terminal: true, MemoryID: "mem-1", MergedAt: "2026-01-01T00:00:00Z"})
	got := rs.SummarizedPRs["42"]
	if got.Skipped {
		t.Errorf("a stored outcome should not be recorded as Skipped: %+v", got)
	}
	if got.MemoryID != "mem-1" {
		t.Errorf("MemoryID = %q, want mem-1", got.MemoryID)
	}

	recordOutcome(&rs, 43, prOutcome{Stored: false, Terminal: true, MergedAt: "2026-01-02T00:00:00Z"})
	got = rs.SummarizedPRs["43"]
	if !got.Skipped {
		t.Errorf("a bot-skipped outcome (Stored=false) should be recorded as Skipped: %+v", got)
	}
	if got.MemoryID != "" {
		t.Errorf("a skipped outcome should have no MemoryID, got %q", got.MemoryID)
	}
}

func TestDiscoverNewPRsCapsAtBudget(t *testing.T) {
	// Four commits map to four distinct PRs; budget=2 should discover only
	// the first two (oldest-first) and leave the rest for the next scan --
	// not marked seen, not resolved further.
	shas := []string{"c1", "c2", "c3", "c4"}
	resolveCalls := 0
	resolve := func(sha string) (int, bool, error) {
		resolveCalls++
		switch sha {
		case "c1":
			return 101, true, nil
		case "c2":
			return 102, true, nil
		case "c3":
			return 103, true, nil
		case "c4":
			return 104, true, nil
		}
		return 0, false, nil
	}

	var rs sourceState
	newPRs, prCommits := discoverNewPRs(&rs, shas, 2, resolve)

	if got, want := newPRs, []int{101, 102}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("newPRs = %v, want %v", got, want)
	}
	if resolveCalls != 2 {
		t.Errorf("resolve called %d times, want 2 (budget-capped, c3/c4 untouched)", resolveCalls)
	}
	if len(prCommits[101]) != 1 || prCommits[101][0] != "c1" {
		t.Errorf("prCommits[101] = %v, want [c1]", prCommits[101])
	}
	if rs.SeenCommits["c1"] || rs.SeenCommits["c2"] {
		t.Errorf("commits belonging to still-undecided PRs must not be marked seen yet")
	}
}

func TestDiscoverNewPRsSkipsDirectPushAndDecided(t *testing.T) {
	rs := sourceState{
		SummarizedPRs: map[string]prRecord{"200": {MemoryID: "mem-200"}},
	}
	shas := []string{"push1", "decided1", "new1"}
	resolve := func(sha string) (int, bool, error) {
		switch sha {
		case "push1":
			return 0, false, nil // direct push, no PR
		case "decided1":
			return 200, true, nil // already-decided PR
		case "new1":
			return 300, true, nil
		}
		t.Fatalf("unexpected sha %q", sha)
		return 0, false, nil
	}

	newPRs, prCommits := discoverNewPRs(&rs, shas, 10, resolve)

	if len(newPRs) != 1 || newPRs[0] != 300 {
		t.Errorf("newPRs = %v, want [300]", newPRs)
	}
	if len(prCommits) != 1 {
		t.Errorf("prCommits = %v, want only PR 300", prCommits)
	}
	if !rs.SeenCommits["push1"] {
		t.Errorf("direct-push commit should be marked seen immediately")
	}
	if !rs.SeenCommits["decided1"] {
		t.Errorf("commit belonging to an already-decided PR should be marked seen immediately")
	}
	if rs.SeenCommits["new1"] {
		t.Errorf("commit belonging to a newly-discovered, still-undecided PR should not be marked seen yet")
	}
}

func TestDiscoverNewPRsSkipsAlreadySeenCommits(t *testing.T) {
	rs := sourceState{SeenCommits: map[string]bool{"c1": true}}
	resolveCalls := 0
	resolve := func(sha string) (int, bool, error) {
		resolveCalls++
		return 999, true, nil
	}

	newPRs, _ := discoverNewPRs(&rs, []string{"c1"}, 10, resolve)

	if resolveCalls != 0 {
		t.Errorf("resolve should not be called for an already-seen commit")
	}
	if len(newPRs) != 0 {
		t.Errorf("newPRs = %v, want empty", newPRs)
	}
}

func TestSaveStateLogsWriteFailure(t *testing.T) {
	dir := t.TempDir()
	// stateFile's parent path element is a regular file, not a directory, so
	// os.MkdirAll(filepath.Dir(stateFile)) is guaranteed to fail.
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatalf("write blocker: %v", err)
	}

	var logBuf strings.Builder
	prevLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logBuf, nil)))
	t.Cleanup(func() { slog.SetDefault(prevLogger) })

	w := &Watcher{stateFile: filepath.Join(blocker, "state.json")}
	w.st = state{Sources: map[string]sourceState{"repo|dir": {}}}
	w.saveState()

	if !strings.Contains(logBuf.String(), "prwatcher: create state dir failed") {
		t.Errorf("saveState should log the MkdirAll failure, got log output: %q", logBuf.String())
	}
}

// TestProcessNewPRsPersistsAfterEachTerminalOutcome regression-tests the
// CRITICAL fix for prwatcher's original bug: state must be persisted after
// EVERY terminal PR outcome, not once at the end of the whole batch, so an
// interruption between two PRs can lose at most the one in flight. It
// simulates exactly that interruption: the fake summarizer errors on the
// third PR, and the test asserts the persisted snapshots already reflect
// PRs 1 and 2 by the time that happens -- proving their progress was never
// dependent on the batch finishing.
func TestProcessNewPRsPersistsAfterEachTerminalOutcome(t *testing.T) {
	rs := &sourceState{}
	newPRs := []int{101, 102, 103}
	prCommits := map[int][]string{
		101: {"sha1"},
		102: {"sha2"},
		103: {"sha3"},
	}

	// persistedSnapshots holds a JSON-marshaled copy taken at the moment of
	// each persist call -- matching what saveState actually does on disk
	// (marshal immediately). sourceState's fields are maps (reference
	// types), so collecting the struct itself instead of marshaling it would
	// alias later mutations back into "earlier" snapshots and defeat the
	// point of this test.
	var persistedSnapshots []sourceState
	summarize := func(num int) (prOutcome, error) {
		if num == 103 {
			return prOutcome{}, errors.New("simulated interruption on PR 3")
		}
		return prOutcome{Stored: true, Terminal: true, MemoryID: "mem-" + strconv.Itoa(num), MergedAt: "2026-01-01T00:00:00Z"}, nil
	}
	persist := func(snapshot sourceState) {
		data, err := json.Marshal(snapshot)
		if err != nil {
			t.Fatalf("marshal snapshot: %v", err)
		}
		var dup sourceState
		if err := json.Unmarshal(data, &dup); err != nil {
			t.Fatalf("unmarshal snapshot: %v", err)
		}
		persistedSnapshots = append(persistedSnapshots, dup)
	}

	processNewPRs(rs, newPRs, prCommits, "acme/widgets", summarize, persist)

	if len(persistedSnapshots) != 2 {
		t.Fatalf("persist called %d times, want 2 (once per terminal PR, before the 3rd PR's error)", len(persistedSnapshots))
	}
	// The FIRST persisted snapshot must already contain PR 101's outcome --
	// proving it was saved immediately, not batched until PR 102 also finished.
	if _, ok := persistedSnapshots[0].SummarizedPRs["101"]; !ok {
		t.Errorf("first persist call should already reflect PR 101, got %+v", persistedSnapshots[0].SummarizedPRs)
	}
	if _, ok := persistedSnapshots[0].SummarizedPRs["102"]; ok {
		t.Errorf("first persist call should NOT yet reflect PR 102 (it hasn't been summarized yet), got %+v", persistedSnapshots[0].SummarizedPRs)
	}
	// The final in-memory state (rs) must reflect both successful PRs, and
	// the failed 3rd PR must be absent -- it will be retried next scan.
	if len(rs.SummarizedPRs) != 2 {
		t.Errorf("rs.SummarizedPRs = %v, want exactly 101 and 102", rs.SummarizedPRs)
	}
	if _, ok := rs.SummarizedPRs["103"]; ok {
		t.Errorf("PR 103 errored and must not be recorded as decided, got %+v", rs.SummarizedPRs)
	}
	if rs.SeenCommits["sha3"] {
		t.Errorf("sha3 (PR 103's commit) must not be marked seen -- it needs to be re-resolved next scan")
	}
}

func TestClassifyPR(t *testing.T) {
	cases := []struct {
		name     string
		state    string
		mergedAt string
		want     prDisposition
	}{
		{"merged", "MERGED", "2026-01-01T00:00:00Z", prMerged},
		{"merged but empty MergedAt is not trusted", "MERGED", "", prStillOpen},
		{"open", "OPEN", "", prStillOpen},
		{"closed without merging", "CLOSED", "", prRejected},
		{"closed with a stray MergedAt is still rejected, not merged", "CLOSED", "2026-01-01T00:00:00Z", prRejected},
	}
	for _, c := range cases {
		if got := classifyPR(c.state, c.mergedAt); got != c.want {
			t.Errorf("%s: classifyPR(%q, %q) = %v, want %v", c.name, c.state, c.mergedAt, got, c.want)
		}
	}
}

func TestExpandHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir available")
	}
	if got, want := expandHome("~/foo/bar"), filepath.Join(home, "foo", "bar"); got != want {
		t.Errorf("expandHome(~/foo/bar) = %q, want %q", got, want)
	}
	if got := expandHome("/abs/path"); got != "/abs/path" {
		t.Errorf("expandHome should leave absolute paths unchanged, got %q", got)
	}
}
