// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package runner

import "time"

// Outcome classifies the terminal state of a single RunOne execution.
type Outcome string

const (
	OutcomePatchGenerated Outcome = "patch_generated" // patch extracted — harness determines pass/fail
	OutcomeNoPatch        Outcome = "no_patch"         // claude produced no extractable patch
	OutcomeClaudeFail     Outcome = "claude_fail"      // claude process error or timeout
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
