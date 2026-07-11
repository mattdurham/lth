// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package prwatcher

import (
	"os"
	"path/filepath"
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
