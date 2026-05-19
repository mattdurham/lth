// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package runner

import (
	"fmt"

	"github.com/mattdurham/lth/internal/bench/dataset"
)

// Approach names the strategy used to prompt Claude.
type Approach string

const (
	ApproachBobWork  Approach = "bob-work"
	ApproachLthWork  Approach = "lth-work"
	ApproachDefault  Approach = "default"
	ApproachLthSingle Approach = "lth-single" // lth prompt context + single focused coder, no team
)

// AllApproaches is the canonical ordered list used when no --approaches flag is given.
var AllApproaches = []Approach{ApproachBobWork, ApproachLthWork, ApproachDefault, ApproachLthSingle}

// BuildPrompt constructs the stdin prompt for `claude -p` for the given problem.
func (a Approach) BuildPrompt(p dataset.Problem) string {
	switch a {
	case ApproachBobWork:
		return buildBobWorkPrompt(p)
	case ApproachLthWork:
		return buildLthWorkPrompt(p)
	case ApproachLthSingle:
		return buildLthSinglePrompt(p)
	default:
		return buildDefaultPrompt(p)
	}
}

func buildDefaultPrompt(p dataset.Problem) string {
	return fmt.Sprintf(
		"You are a software engineer. Here is a GitHub issue:\n\n<issue>\n%s\n</issue>\n\n"+
			"You are working inside the actual repository. Read the relevant source files, "+
			"identify the bug, and fix it by editing the files directly using your file tools. "+
			"Do not output a patch — your file changes will be captured automatically.",
		p.ProblemStatement,
	)
}

func buildBobWorkPrompt(p dataset.Problem) string {
	return fmt.Sprintf(
		"Before starting, search lth for prior experience with this repo and any files you touch:\n"+
			"  ~/bin/lth search \"%s\" --layers L3,L4,L5 --top 5\n"+
			"  # Before editing any file: ~/bin/lth read <filepath>\n\n"+
			"/bob:work \"%s\"\n\n"+
			"You are working inside the actual repository. Edit files directly — changes are captured automatically via git diff.",
		p.Repo, p.ProblemStatement,
	)
}

// buildLthSinglePrompt injects lth memory context via `lth prompt` then gives
// a single focused instruction to fix the issue and output a patch.
// No team spawning — faster and less overhead than full lth-work workflow.
func buildLthSinglePrompt(p dataset.Problem) string {
	return fmt.Sprintf(
		"First, run this to get relevant context from memory:\n"+
			"  ~/bin/lth prompt \"%s\" --top-each 3 --ppr=false --expand=false\n\n"+
			"Apply what you find as context, then fix this GitHub issue:\n\n"+
			"<issue>\n%s\n</issue>\n\n"+
			"You are working inside the actual repository. Read the relevant source files using "+
			"your file tools, identify the bug, and fix it by editing the files directly. "+
			"Do not output a patch — your file changes will be captured automatically.",
		p.Repo+" "+p.ProblemStatement[:min(len(p.ProblemStatement), 100)],
		p.ProblemStatement,
	)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func buildLthWorkPrompt(p dataset.Problem) string {
	return fmt.Sprintf(
		"Before starting, search lth for prior experience with this repo and any files you touch:\n"+
			"  ~/bin/lth search \"%s\" --layers L3,L4,L5 --top 5\n"+
			"  # Before editing any file: ~/bin/lth read <filepath>\n\n"+
			"/lth-work \"%s\"\n\n"+
			"You are working inside the actual repository. Edit files directly — changes are captured automatically via git diff.",
		p.Repo, p.ProblemStatement,
	)
}
