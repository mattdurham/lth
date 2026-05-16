// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package predictions

import (
	"encoding/json"
	"fmt"
	"os"
)

// Prediction is one entry in the SWE-bench predictions.jsonl output file.
type Prediction struct {
	InstanceID string `json:"instance_id"`
	ModelPatch string `json:"model_patch"`
	ModelName  string `json:"model_name_or_path"`
}

// Writer appends Prediction records to a JSONL file.
type Writer struct {
	f *os.File
}

// NewWriter opens or creates path in append mode.
func NewWriter(path string) (*Writer, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("open predictions file: %w", err)
	}
	return &Writer{f: f}, nil
}

// Append marshals p as JSON and writes it as one line.
func (w *Writer) Append(p Prediction) error {
	b, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("marshal prediction: %w", err)
	}
	if _, err := w.f.Write(append(b, '\n')); err != nil {
		return fmt.Errorf("write prediction: %w", err)
	}
	return nil
}

// Close closes the underlying file.
func (w *Writer) Close() error {
	return w.f.Close()
}

// PredictionsPath returns the standard file path for a given approach's predictions file.
func PredictionsPath(approach string) string {
	return "predictions-" + approach + ".jsonl"
}
