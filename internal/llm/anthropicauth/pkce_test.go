// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package anthropicauth

import (
	"crypto/sha256"
	"encoding/base64"
	"testing"
)

func TestGeneratePKCE(t *testing.T) {
	p, err := GeneratePKCE()
	if err != nil {
		t.Fatalf("GeneratePKCE: %v", err)
	}
	if len(p.Verifier) < 43 || len(p.Verifier) > 128 {
		t.Errorf("verifier length %d outside RFC7636 bounds", len(p.Verifier))
	}
	// challenge must be SHA256(verifier) base64url(no pad)
	sum := sha256.Sum256([]byte(p.Verifier))
	want := base64.RawURLEncoding.EncodeToString(sum[:])
	if p.Challenge != want {
		t.Errorf("challenge mismatch:\n got %q\nwant %q", p.Challenge, want)
	}
}

func TestGeneratePKCE_unique(t *testing.T) {
	a, _ := GeneratePKCE()
	b, _ := GeneratePKCE()
	if a.Verifier == b.Verifier {
		t.Error("two PKCE generations produced identical verifiers")
	}
}
