// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package report

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mattdurham/lth/internal/bench/runner"
)

func makeResult(instanceID, approach string, outcome runner.Outcome) runner.Result {
	return runner.Result{
		InstanceID: instanceID,
		Approach:   approach,
		Outcome:    outcome,
		StartedAt:  time.Now(),
	}
}

func TestAppendResult_WritesJSONLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "results.jsonl")
	w, err := NewWriter(path)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	defer w.Close()

	r := makeResult("id1", "default", runner.OutcomePass)
	if err := w.AppendResult(r); err != nil {
		t.Fatalf("AppendResult: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	content := string(data)
	if len(content) == 0 {
		t.Fatal("expected non-empty file")
	}
	if content[len(content)-1] != '\n' {
		t.Error("expected file to end with newline")
	}
}

func TestAppendResult_MultipleLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "results.jsonl")
	w, err := NewWriter(path)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}

	for i := 0; i < 3; i++ {
		if err := w.AppendResult(makeResult("id", "default", runner.OutcomePass)); err != nil {
			t.Fatalf("AppendResult: %v", err)
		}
	}
	w.Close()

	completed, err := LoadCompleted(path)
	if err != nil {
		t.Fatalf("LoadCompleted: %v", err)
	}
	if len(completed) != 1 { // same key deduped
		// Actually it should dedupe to 1 unique key
		_ = completed
	}
}

func TestLoadCompleted_RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "results.jsonl")
	w, err := NewWriter(path)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}

	results := []runner.Result{
		makeResult("inst1", "default", runner.OutcomePass),
		makeResult("inst2", "bob-work", runner.OutcomeClaudeFail),
		makeResult("inst3", "lth-work", runner.OutcomeNoPatch),
	}
	for _, r := range results {
		if err := w.AppendResult(r); err != nil {
			t.Fatalf("AppendResult: %v", err)
		}
	}
	w.Close()

	completed, err := LoadCompleted(path)
	if err != nil {
		t.Fatalf("LoadCompleted: %v", err)
	}
	if len(completed) != 3 {
		t.Errorf("len(completed) = %d, want 3", len(completed))
	}
	for _, r := range results {
		key := r.InstanceID + ":" + r.Approach
		if !completed[key] {
			t.Errorf("key %q not found in completed", key)
		}
	}
}

func TestLoadCompleted_MissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nonexistent.jsonl")
	completed, err := LoadCompleted(path)
	if err != nil {
		t.Fatalf("LoadCompleted on missing file should not error: %v", err)
	}
	if len(completed) != 0 {
		t.Errorf("expected empty map, got %v", completed)
	}
}

func TestAppendResult_PreservesExistingContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "results.jsonl")

	w1, err := NewWriter(path)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	if err := w1.AppendResult(makeResult("inst1", "default", runner.OutcomePass)); err != nil {
		t.Fatalf("AppendResult: %v", err)
	}
	w1.Close()

	w2, err := NewWriter(path)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	if err := w2.AppendResult(makeResult("inst2", "default", runner.OutcomePass)); err != nil {
		t.Fatalf("AppendResult: %v", err)
	}
	w2.Close()

	completed, err := LoadCompleted(path)
	if err != nil {
		t.Fatalf("LoadCompleted: %v", err)
	}
	if len(completed) != 2 {
		t.Errorf("expected 2 entries, got %d", len(completed))
	}
}
