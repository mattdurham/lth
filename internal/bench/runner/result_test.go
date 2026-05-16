// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package runner

import (
	"encoding/json"
	"testing"
	"time"
)

func TestOutcomePassSerializesToPass(t *testing.T) {
	b, err := json.Marshal(OutcomePass)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `"pass"` {
		t.Errorf("got %s, want \"pass\"", b)
	}
}

func TestResultRoundTrip(t *testing.T) {
	r := Result{
		InstanceID:  "gin-gonic__gin-1234",
		Approach:    "default",
		Outcome:     OutcomePass,
		ModelPatch:  "diff --git a/foo.go b/foo.go\n--- a/foo.go\n+++ b/foo.go",
		DurationSec: 42.5,
		Error:       "some error",
		StartedAt:   time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC),
	}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var got Result
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got.InstanceID != r.InstanceID {
		t.Errorf("InstanceID: got %q, want %q", got.InstanceID, r.InstanceID)
	}
	if got.Approach != r.Approach {
		t.Errorf("Approach: got %q, want %q", got.Approach, r.Approach)
	}
	if got.Outcome != r.Outcome {
		t.Errorf("Outcome: got %q, want %q", got.Outcome, r.Outcome)
	}
	if got.ModelPatch != r.ModelPatch {
		t.Errorf("ModelPatch: got %q, want %q", got.ModelPatch, r.ModelPatch)
	}
	if got.DurationSec != r.DurationSec {
		t.Errorf("DurationSec: got %f, want %f", got.DurationSec, r.DurationSec)
	}
	if got.Error != r.Error {
		t.Errorf("Error: got %q, want %q", got.Error, r.Error)
	}
	if !got.StartedAt.Equal(r.StartedAt) {
		t.Errorf("StartedAt: got %v, want %v", got.StartedAt, r.StartedAt)
	}
}

func TestAllOutcomeConstants(t *testing.T) {
	tests := []struct {
		outcome  Outcome
		expected string
	}{
		{OutcomePass, "pass"},
		{OutcomeNoPatch, "no_patch"},
		{OutcomeClaudeFail, "claude_fail"},
	}
	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if string(tt.outcome) != tt.expected {
				t.Errorf("got %q, want %q", tt.outcome, tt.expected)
			}
			b, err := json.Marshal(tt.outcome)
			if err != nil {
				t.Fatal(err)
			}
			want := `"` + tt.expected + `"`
			if string(b) != want {
				t.Errorf("JSON: got %s, want %s", b, want)
			}
		})
	}
}
