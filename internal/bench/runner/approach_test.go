// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package runner

import (
	"strings"
	"testing"

	"github.com/mattdurham/lth/internal/bench/dataset"
)

func TestBuildPrompt_Default(t *testing.T) {
	p := dataset.Problem{ProblemStatement: "fix the bug"}
	prompt := ApproachDefault.BuildPrompt(p)
	if !strings.Contains(prompt, "<issue>") {
		t.Error("default prompt missing <issue> tag")
	}
	if !strings.Contains(prompt, "</issue>") {
		t.Error("default prompt missing </issue> tag")
	}
	if !strings.Contains(prompt, "edit") && !strings.Contains(prompt, "file") {
		t.Error("default prompt should instruct agent to edit files")
	}
	if !strings.Contains(prompt, "fix the bug") {
		t.Error("default prompt missing problem statement")
	}
}

func TestBuildPrompt_BobWork(t *testing.T) {
	p := dataset.Problem{ProblemStatement: "add a feature"}
	prompt := ApproachBobWork.BuildPrompt(p)
	if !strings.Contains(prompt, "/bob:work") {
		t.Error("bob-work prompt missing /bob:work invocation")
	}
	if !strings.Contains(prompt, "add a feature") {
		t.Error("bob-work prompt missing problem statement")
	}
}

func TestBuildPrompt_LthWork(t *testing.T) {
	p := dataset.Problem{ProblemStatement: "refactor something"}
	prompt := ApproachLthWork.BuildPrompt(p)
	if !strings.Contains(prompt, "/lth-work") {
		t.Error("lth-work prompt missing /lth-work invocation")
	}
	if !strings.Contains(prompt, "refactor something") {
		t.Error("lth-work prompt missing problem statement")
	}
}

func TestAllApproaches_Length(t *testing.T) {
	if len(AllApproaches) < 3 {
		t.Errorf("AllApproaches length = %d, want at least 3", len(AllApproaches))
	}
}

func TestBuildPrompt_Interpolation(t *testing.T) {
	stmt := "unique-problem-statement-xyz"
	p := dataset.Problem{ProblemStatement: stmt}
	for _, a := range AllApproaches {
		prompt := a.BuildPrompt(p)
		if !strings.Contains(prompt, stmt) {
			t.Errorf("approach %q prompt missing problem statement", a)
		}
	}
}
