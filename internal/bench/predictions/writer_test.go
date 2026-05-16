// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package predictions

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestWriterAppendAndFormat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "predictions.jsonl")
	w, err := NewWriter(path)
	if err != nil {
		t.Fatal(err)
	}

	p1 := Prediction{InstanceID: "astropy__astropy-1", ModelPatch: "diff --git a/x", ModelName: "lth-work"}
	p2 := Prediction{InstanceID: "gin-gonic__gin-2", ModelPatch: "diff --git a/y", ModelName: "default"}

	if err := w.Append(p1); err != nil {
		t.Fatal(err)
	}
	if err := w.Append(p2); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	var lines []Prediction
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var p Prediction
		if err := json.Unmarshal(scanner.Bytes(), &p); err != nil {
			t.Fatalf("unmarshal line: %v", err)
		}
		lines = append(lines, p)
	}

	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2", len(lines))
	}
	if lines[0].InstanceID != p1.InstanceID {
		t.Errorf("line 0 InstanceID = %q, want %q", lines[0].InstanceID, p1.InstanceID)
	}
	if lines[0].ModelPatch != p1.ModelPatch {
		t.Errorf("line 0 ModelPatch = %q, want %q", lines[0].ModelPatch, p1.ModelPatch)
	}
	if lines[0].ModelName != p1.ModelName {
		t.Errorf("line 0 ModelName = %q, want %q", lines[0].ModelName, p1.ModelName)
	}
	if lines[1].InstanceID != p2.InstanceID {
		t.Errorf("line 1 InstanceID = %q, want %q", lines[1].InstanceID, p2.InstanceID)
	}
}

func TestWriterJSONFieldNames(t *testing.T) {
	path := filepath.Join(t.TempDir(), "predictions.jsonl")
	w, err := NewWriter(path)
	if err != nil {
		t.Fatal(err)
	}
	p := Prediction{InstanceID: "foo__bar-1", ModelPatch: "patch", ModelName: "mymodel"}
	if err := w.Append(p); err != nil {
		t.Fatal(err)
	}
	w.Close()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	var raw map[string]string
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	if raw["instance_id"] != "foo__bar-1" {
		t.Errorf("instance_id = %q", raw["instance_id"])
	}
	if raw["model_patch"] != "patch" {
		t.Errorf("model_patch = %q", raw["model_patch"])
	}
	if raw["model_name_or_path"] != "mymodel" {
		t.Errorf("model_name_or_path = %q", raw["model_name_or_path"])
	}
}
