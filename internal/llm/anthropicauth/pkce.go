// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package anthropicauth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
)

// PKCE holds a verifier/challenge pair for an OAuth PKCE flow.
type PKCE struct {
	Verifier  string
	Challenge string
}

// GeneratePKCE creates a new PKCE verifier and S256 challenge.
// The verifier is 32 random bytes, base64url-encoded (no padding) -> 43 chars.
// The challenge is SHA256(verifier), base64url-encoded (no padding).
func GeneratePKCE() (PKCE, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return PKCE{}, fmt.Errorf("read random: %w", err)
	}
	verifier := base64.RawURLEncoding.EncodeToString(buf)
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	return PKCE{Verifier: verifier, Challenge: challenge}, nil
}
