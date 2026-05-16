// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package report

import (
	"strings"
	"testing"

	"github.com/mattdurham/lth/internal/bench/runner"
)

func TestPrintSummary_Empty(t *testing.T) {
	var buf strings.Builder
	PrintSummary(nil, &buf)
	if !strings.Contains(buf.String(), "no results") {
		t.Errorf("expected 'no results' in output, got: %q", buf.String())
	}
}

func TestPrintSummary_AllPatches(t *testing.T) {
	results := []runner.Result{
		{InstanceID: "i1", Approach: "default", Outcome: runner.OutcomePatchGenerated},
		{InstanceID: "i2", Approach: "default", Outcome: runner.OutcomePatchGenerated},
	}
	var buf strings.Builder
	PrintSummary(results, &buf)
	out := buf.String()
	if !strings.Contains(out, "100.0%") {
		t.Errorf("expected 100.0%% patch rate, got: %q", out)
	}
}

func TestPrintSummary_MixedOutcomes(t *testing.T) {
	results := []runner.Result{
		{InstanceID: "i1", Approach: "bob-work", Outcome: runner.OutcomePatchGenerated},
		{InstanceID: "i2", Approach: "bob-work", Outcome: runner.OutcomePatchGenerated},
		{InstanceID: "i3", Approach: "bob-work", Outcome: runner.OutcomeNoPatch},
		{InstanceID: "i4", Approach: "bob-work", Outcome: runner.OutcomeClaudeFail},
		{InstanceID: "i5", Approach: "bob-work", Outcome: runner.OutcomeClaudeFail},
	}
	var buf strings.Builder
	PrintSummary(results, &buf)
	out := buf.String()
	if !strings.Contains(out, "bob-work") {
		t.Errorf("expected approach name in output, got: %q", out)
	}
	// 2 patches out of 5 = 40.0%
	if !strings.Contains(out, "40.0%") {
		t.Errorf("expected 40.0%% patch rate, got: %q", out)
	}
}

func TestPrintSummary_WritesToWriter(t *testing.T) {
	var buf strings.Builder
	results := []runner.Result{
		{InstanceID: "i1", Approach: "default", Outcome: runner.OutcomePatchGenerated},
	}
	PrintSummary(results, &buf)
	if buf.Len() == 0 {
		t.Error("expected output written to writer, got nothing")
	}
}
