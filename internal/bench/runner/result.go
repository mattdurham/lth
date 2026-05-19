// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package runner

import "time"

// Outcome classifies the terminal state of a single RunOne execution.
type Outcome string

const (
	OutcomePatchGenerated  Outcome = "patch_generated"   // patch captured via git diff
	OutcomeNoPatch         Outcome = "no_patch"           // no changes detected after claude ran
	OutcomeClaudeFail      Outcome = "claude_fail"        // claude process error or timeout
	OutcomeCloneFail       Outcome = "clone_fail"         // git clone or worktree setup failed
	OutcomeTestPatchFail   Outcome = "test_patch_fail"    // applying test_patch failed
)

// Result holds the outcome of one problem × approach run.
type Result struct {
	InstanceID  string    `json:"instance_id"`
	Approach    string    `json:"approach"`
	Outcome     Outcome   `json:"outcome"`
	ModelPatch  string    `json:"model_patch,omitempty"`
	DurationSec float64   `json:"duration_sec"`
	Error       string    `json:"error,omitempty"`
	StartedAt   time.Time `json:"started_at"`
}
