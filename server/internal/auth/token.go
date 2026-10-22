package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"strings"
)

// NewToken returns a 256-bit session token (base64url) and the SHA-256 hash that is stored.
func NewToken() (plain string, hash []byte) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand failing means the system is unusable
	}
	plain = base64.RawURLEncoding.EncodeToString(b)
	return plain, HashToken(plain)
}

func HashToken(plain string) []byte {
	h := sha256.Sum256([]byte(plain))
	return h[:]
}

var invB32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// newInviteCode returns a human-typeable code like "K3QX-9MZA-T7PL-2WBD" (80 bits).
func newInviteCode() string {
	b := make([]byte, 10)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	s := invB32.EncodeToString(b)
	return s[0:4] + "-" + s[4:8] + "-" + s[8:12] + "-" + s[12:16]
}

// hashInvite normalises case, dashes and whitespace before hashing, so people can type codes loosely.
func hashInvite(code string) []byte {
	norm := strings.ToUpper(strings.NewReplacer("-", "", " ", "").Replace(strings.TrimSpace(code)))
	h := sha256.Sum256([]byte(norm))
	return h[:]
}
