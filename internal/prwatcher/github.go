// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package prwatcher

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
)

// ghPRRef is the minimal shape of an entry in the GitHub "list PRs
// associated with a commit" response.
type ghPRRef struct {
	Number int `json:"number"`
}

// resolvePRForCommit returns the PR number associated with sha in repo, if
// any. A commit reached only by direct push (no PR) returns ok=false.
func resolvePRForCommit(repo, sha string) (number int, ok bool, err error) {
	out, err := exec.Command("gh", "api", fmt.Sprintf("repos/%s/commits/%s/pulls", repo, sha)).Output() //nolint:gosec // repo/sha come from config and local git log, not untrusted input
	if err != nil {
		return 0, false, fmt.Errorf("gh api commit pulls: %w", err)
	}
	var refs []ghPRRef
	if err := json.Unmarshal(out, &refs); err != nil {
		return 0, false, fmt.Errorf("parse commit pulls: %w", err)
	}
	if len(refs) == 0 {
		return 0, false, nil
	}
	return refs[0].Number, true, nil
}

// ghAuthor is the author sub-object of a `gh pr view` JSON response.
type ghAuthor struct {
	Login string `json:"login"`
}

// ghPR is the subset of `gh pr view --json` fields prwatcher needs.
type ghPR struct {
	Number   int      `json:"number"`
	Title    string   `json:"title"`
	Body     string   `json:"body"`
	URL      string   `json:"url"`
	State    string   `json:"state"`
	MergedAt string   `json:"mergedAt"`
	Author   ghAuthor `json:"author"`
}

// fetchPR fetches PR metadata via `gh pr view`.
func fetchPR(repo string, number int) (*ghPR, error) {
	out, err := exec.Command("gh", "pr", "view", strconv.Itoa(number), "--repo", repo, //nolint:gosec // repo/number come from config and gh's own commit-pulls response
		"--json", "number,title,body,url,state,mergedAt,author").Output()
	if err != nil {
		return nil, fmt.Errorf("gh pr view: %w", err)
	}
	var pr ghPR
	if err := json.Unmarshal(out, &pr); err != nil {
		return nil, fmt.Errorf("parse pr view: %w", err)
	}
	return &pr, nil
}

// fetchDiff fetches the unified diff for a PR via `gh pr diff`.
func fetchDiff(repo string, number int) (string, error) {
	out, err := exec.Command("gh", "pr", "diff", strconv.Itoa(number), "--repo", repo).Output() //nolint:gosec // repo/number come from config and gh's own commit-pulls response
	if err != nil {
		return "", fmt.Errorf("gh pr diff: %w", err)
	}
	return string(out), nil
}
