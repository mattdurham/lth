// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// Package gwswatcher periodically pulls Google Workspace meeting notes and
// transcripts into a local directory via the `gws` CLI (Google Workspace CLI,
// https://www.npmjs.com/package/google-workspace-cli), then leans on the
// markdown watcher to ingest them as L3 memories.
//
// All authentication is delegated to `gws`. lth never reads, stores, or
// transmits Google credentials.
package gwswatcher

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"
)

// gwsRunner is the gws-CLI boundary. Production wraps exec.CommandContext;
// tests inject a stub.
type gwsRunner interface {
	// Run invokes gws with args and returns the raw stdout bytes on success.
	// stderr is included in the returned error on failure.
	Run(ctx context.Context, args ...string) ([]byte, error)
}

// execRunner shells out to a real gws binary. The binary path is resolved at
// construction time; if not on PATH, New() returns an error so the daemon
// can log and skip.
type execRunner struct {
	binary  string
	timeout time.Duration
}

func newExecRunner(binary string, timeout time.Duration) (*execRunner, error) {
	if binary == "" {
		binary = "gws"
	}
	resolved, err := exec.LookPath(binary)
	if err != nil {
		return nil, fmt.Errorf("gws binary not found on PATH (%q): %w", binary, err)
	}
	return &execRunner{binary: resolved, timeout: timeout}, nil
}

func (r *execRunner) Run(ctx context.Context, args ...string) ([]byte, error) {
	runCtx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, r.binary, args...) //nolint:gosec // G204: gws binary path is resolved via exec.LookPath at construction
	out, err := cmd.Output()
	if err != nil {
		stderr := ""
		if exitErr, ok := err.(*exec.ExitError); ok {
			stderr = string(exitErr.Stderr)
		}
		return nil, fmt.Errorf("gws %v: %w (stderr: %s)", args, err, stderr)
	}
	return out, nil
}

// driveFile is the subset of Drive file metadata we use.
type driveFile struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	MimeType     string `json:"mimeType"`
	ModifiedTime string `json:"modifiedTime"`
	WebViewLink  string `json:"webViewLink"`
}

// listMatching queries Drive for documents whose name contains any of
// namePatterns and were modified after sinceUTC. Returns up to pageSize
// results, newest first.
func listMatching(ctx context.Context, r gwsRunner, namePatterns, excludePatterns []string, sinceUTC time.Time, pageSize int) ([]driveFile, error) {
	q := buildDriveQuery(namePatterns, excludePatterns, sinceUTC)
	params := map[string]any{
		"q":        q,
		"pageSize": pageSize,
		"fields":   "files(id,name,mimeType,modifiedTime,webViewLink)",
		"orderBy":  "modifiedTime desc",
	}
	paramsJSON, _ := json.Marshal(params)

	out, err := r.Run(ctx, "drive", "files", "list", "--params", string(paramsJSON), "--format", "json")
	if err != nil {
		return nil, err
	}
	var resp struct {
		Files []driveFile `json:"files"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return nil, fmt.Errorf("decode drive list response: %w", err)
	}
	return resp.Files, nil
}

// fetchDoc retrieves the full Google Docs body JSON for the given doc ID.
func fetchDoc(ctx context.Context, r gwsRunner, docID string) (*docResponse, error) {
	params := map[string]any{"documentId": docID}
	paramsJSON, _ := json.Marshal(params)
	out, err := r.Run(ctx, "docs", "documents", "get", "--params", string(paramsJSON), "--format", "json")
	if err != nil {
		return nil, err
	}
	var d docResponse
	if err := json.Unmarshal(out, &d); err != nil {
		return nil, fmt.Errorf("decode docs response: %w", err)
	}
	return &d, nil
}
