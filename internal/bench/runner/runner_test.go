// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package runner

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mattdurham/lth/internal/bench/dataset"
)

func setupFakeClaude(t *testing.T, patch string) string {
	t.Helper()
	fakeDir := t.TempDir()
	script := "#!/bin/sh\n" +
		"cat /dev/stdin > /dev/null\n" +
		"printf '<patch>\\n%s\\n</patch>\\n' '" + patch + "'\n"
	if err := os.WriteFile(filepath.Join(fakeDir, "claude"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	return fakeDir
}

func TestRunOneReturnsPatch(t *testing.T) {
	expectedPatch := "diff --git a/foo.go b/foo.go"
	fakeDir := setupFakeClaude(t, expectedPatch)
	t.Setenv("PATH", fakeDir+":"+os.Getenv("PATH"))

	r := New(Config{ClaudeTimeout: 30 * time.Second})
	problem := dataset.Problem{
		InstanceID:       "gin-gonic__gin-1",
		Repo:             "gin-gonic/gin",
		ProblemStatement: "fix something",
	}
	result := r.RunOne(context.Background(), problem, ApproachDefault)

	if result.Outcome == OutcomeClaudeFail {
		t.Fatalf("unexpected ClaudeFail: %s", result.Error)
	}
	if result.Outcome != OutcomePatchGenerated {
		t.Errorf("outcome = %q, want %q", result.Outcome, OutcomePatchGenerated)
	}
	if result.ModelPatch == "" {
		t.Error("ModelPatch should not be empty")
	}
	if result.InstanceID != problem.InstanceID {
		t.Errorf("InstanceID = %q, want %q", result.InstanceID, problem.InstanceID)
	}
	if result.Approach != string(ApproachDefault) {
		t.Errorf("Approach = %q, want %q", result.Approach, ApproachDefault)
	}
	if result.DurationSec <= 0 {
		t.Errorf("DurationSec should be positive, got %f", result.DurationSec)
	}
}

func TestRunOneNoPatch(t *testing.T) {
	fakeDir := t.TempDir()
	script := "#!/bin/sh\ncat /dev/stdin > /dev/null\necho 'I could not find a fix'\n"
	if err := os.WriteFile(filepath.Join(fakeDir, "claude"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fakeDir+":"+os.Getenv("PATH"))

	r := New(Config{ClaudeTimeout: 30 * time.Second})
	problem := dataset.Problem{InstanceID: "test-1", ProblemStatement: "fix it"}
	result := r.RunOne(context.Background(), problem, ApproachDefault)

	if result.Outcome != OutcomeNoPatch {
		t.Errorf("outcome = %q, want %q", result.Outcome, OutcomeNoPatch)
	}
	if result.ModelPatch != "" {
		t.Errorf("ModelPatch should be empty for NoPatch outcome, got %q", result.ModelPatch)
	}
}

func TestRunOneClaudeTimeout(t *testing.T) {
	fakeDir := t.TempDir()
	script := "#!/bin/sh\nsleep 300\n"
	if err := os.WriteFile(filepath.Join(fakeDir, "claude"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fakeDir+":"+os.Getenv("PATH"))

	r := New(Config{ClaudeTimeout: 200 * time.Millisecond})
	problem := dataset.Problem{InstanceID: "test-timeout", ProblemStatement: "fix it"}
	result := r.RunOne(context.Background(), problem, ApproachDefault)

	if result.Outcome != OutcomeClaudeFail {
		t.Errorf("outcome = %q, want %q", result.Outcome, OutcomeClaudeFail)
	}
}
