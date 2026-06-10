// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package anthropicauth

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_missing(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "nope.json"))
	if !errors.Is(err, ErrNoCredentials) {
		t.Errorf("want ErrNoCredentials, got %v", err)
	}
}

func TestSaveLoad_roundtrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "creds.json")
	in := &Credentials{Access: "a", Refresh: "r", ExpiresMs: 1234}
	if err := Save(path, in); err != nil {
		t.Fatalf("Save: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("file mode = %v, want 0600", info.Mode().Perm())
	}
	out, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if *out != *in {
		t.Errorf("roundtrip: got %+v want %+v", out, in)
	}
}

func TestLoad_incomplete(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.json")
	if err := os.WriteFile(path, []byte(`{"access":"a"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Error("expected error for missing refresh")
	}
}

func TestDelete_idempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nope.json")
	if err := Delete(path); err != nil {
		t.Errorf("Delete on missing file: %v", err)
	}
}
