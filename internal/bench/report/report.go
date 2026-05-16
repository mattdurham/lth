// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package report

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"

	"github.com/mattdurham/lth/internal/bench/runner"
)

// Writer appends Result records to a JSONL file.
type Writer struct {
	f *os.File
}

// NewWriter opens or creates path in append mode.
func NewWriter(path string) (*Writer, error) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	return &Writer{f: f}, nil
}

// AppendResult marshals r as JSON and writes it as one line.
func (w *Writer) AppendResult(r runner.Result) error {
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	_, err = w.f.Write(data)
	return err
}

// Close closes the underlying file.
func (w *Writer) Close() error {
	return w.f.Close()
}

// LoadCompleted reads path and returns a set of "instanceID:approach" keys
// for results that have already been recorded (any outcome).
// Returns an empty map (not an error) when the file does not exist.
func LoadCompleted(path string) (map[string]bool, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]bool{}, nil
		}
		return nil, err
	}
	defer f.Close()

	completed := map[string]bool{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var r runner.Result
		if err := json.Unmarshal(line, &r); err != nil {
			continue
		}
		key := r.InstanceID + ":" + r.Approach
		completed[key] = true
	}
	return completed, scanner.Err()
}
