// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package runner

import (
	"fmt"

	"github.com/mattdurham/lth/internal/bench/dataset"
)

// Approach names the strategy used to prompt Claude.
type Approach string

const (
	ApproachBobWork Approach = "bob-work"
	ApproachLthWork Approach = "lth-work"
	ApproachDefault Approach = "default"
)

// AllApproaches is the canonical ordered list used when no --approaches flag is given.
var AllApproaches = []Approach{ApproachBobWork, ApproachLthWork, ApproachDefault}

// BuildPrompt constructs the stdin prompt for `claude -p` for the given problem.
func (a Approach) BuildPrompt(p dataset.Problem) string {
	switch a {
	case ApproachBobWork:
		return buildBobWorkPrompt(p)
	case ApproachLthWork:
		return buildLthWorkPrompt(p)
	default:
		return buildDefaultPrompt(p)
	}
}

func buildDefaultPrompt(p dataset.Problem) string {
	return fmt.Sprintf(
		"You are a software engineer. Here is a GitHub issue:\n\n<issue>\n%s\n</issue>\n\nFix the issue by modifying the repository. Output your fix as a unified diff in git diff format, wrapped in <patch>...</patch> XML tags. Output the patch and nothing else between those tags.",
		p.ProblemStatement,
	)
}

func buildBobWorkPrompt(p dataset.Problem) string {
	return fmt.Sprintf(
		"Before starting, search lth for prior experience with this repo and any files you touch:\n"+
			"  ~/bin/lth search \"%s\" --layers L3,L4,L5 --top 5\n"+
			"  # Before editing any file, also run: ~/bin/lth search \"<filename>\" --layers L3,L4,L5 --top 5\n\n"+
			"/bob:work \"%s\"\n\n"+
			"After completing the work, output your fix as a unified diff in git diff format, wrapped in <patch>...</patch> XML tags.",
		p.Repo, p.ProblemStatement,
	)
}

func buildLthWorkPrompt(p dataset.Problem) string {
	return fmt.Sprintf(
		"Before starting, search lth for prior experience with this repo and any files you touch:\n"+
			"  ~/bin/lth search \"%s\" --layers L3,L4,L5 --top 5\n"+
			"  # Before editing any file, also run: ~/bin/lth search \"<filename>\" --layers L3,L4,L5 --top 5\n\n"+
			"/lth-work \"%s\"\n\n"+
			"After completing the work, output your fix as a unified diff in git diff format, wrapped in <patch>...</patch> XML tags.",
		p.Repo, p.ProblemStatement,
	)
}
