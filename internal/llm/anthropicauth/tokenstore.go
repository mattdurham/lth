// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package anthropicauth

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Credentials are the OAuth credentials persisted to disk.
type Credentials struct {
	Access  string `json:"access"`
	Refresh string `json:"refresh"`
	// ExpiresMs is the access token expiry as Unix milliseconds.
	ExpiresMs int64 `json:"expires"`
}

// ErrNoCredentials is returned when no credentials file exists.
var ErrNoCredentials = errors.New("no anthropic oauth credentials; run `lth auth login`")

// DefaultPath returns the canonical credentials file path: ~/.lth/anthropic-oauth.json.
func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home dir: %w", err)
	}
	return filepath.Join(home, ".lth", "anthropic-oauth.json"), nil
}

// Load reads credentials from path. Returns ErrNoCredentials if the file is missing.
func Load(path string) (*Credentials, error) {
	data, err := os.ReadFile(path) //nolint:gosec // user-provided config path
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrNoCredentials
		}
		return nil, fmt.Errorf("read credentials: %w", err)
	}
	var c Credentials
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse credentials: %w", err)
	}
	if c.Access == "" || c.Refresh == "" {
		return nil, fmt.Errorf("credentials file at %s is missing access/refresh", path)
	}
	return &c, nil
}

// Save writes credentials to path with mode 0600. Parent directory is created if missing.
func Save(path string, c *Credentials) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal credentials: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write credentials: %w", err)
	}
	return nil
}

// Delete removes the credentials file. No-op if it doesn't exist.
func Delete(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("delete credentials: %w", err)
	}
	return nil
}
