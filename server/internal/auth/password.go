package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"

	"golang.org/x/crypto/argon2"
)

// Params are Argon2id cost parameters. Memory is in KiB.
type Params struct {
	Memory  uint32
	Time    uint32
	Threads uint8
}

// DefaultParams: m=64 MiB, t=3, p=2 (spec §5.3).
var DefaultParams = Params{Memory: 64 * 1024, Time: 3, Threads: 2}

var (
	params    = DefaultParams
	dummyHash string
	dummyOnce sync.Once
	// hashSlots caps concurrent Argon2 work: each hash allocates Params.Memory, so
	// unbounded parallel logins would be a cheap memory-exhaustion attack.
	hashSlots = make(chan struct{}, 4)
)

// UseParamsForTesting lowers Argon2 cost so test suites stay fast. Never call it in production code.
func UseParamsForTesting(p Params) {
	params = p
	dummyOnce = sync.Once{}
}

const saltLen, keyLen = 16, 32

var b64 = base64.RawStdEncoding

func HashPassword(pw string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	hashSlots <- struct{}{}
	key := argon2.IDKey([]byte(pw), salt, params.Time, params.Memory, params.Threads, keyLen)
	<-hashSlots
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, params.Memory, params.Time, params.Threads, b64.EncodeToString(salt), b64.EncodeToString(key)), nil
}

// VerifyPassword checks pw against a PHC-encoded Argon2id hash in constant time.
func VerifyPassword(phc, pw string) (bool, error) {
	parts := strings.Split(phc, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, errors.New("auth: malformed password hash")
	}
	var v int
	var p Params
	if _, err := fmt.Sscanf(parts[2], "v=%d", &v); err != nil || v != argon2.Version {
		return false, errors.New("auth: unsupported argon2 version")
	}
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.Memory, &p.Time, &p.Threads); err != nil {
		return false, errors.New("auth: malformed argon2 params")
	}
	salt, err1 := b64.DecodeString(parts[4])
	want, err2 := b64.DecodeString(parts[5])
	if err1 != nil || err2 != nil || len(want) == 0 {
		return false, errors.New("auth: malformed argon2 salt or key")
	}
	hashSlots <- struct{}{}
	got := argon2.IDKey([]byte(pw), salt, p.Time, p.Memory, p.Threads, uint32(len(want)))
	<-hashSlots
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

// DummyVerify burns the same Argon2 cost as a real verification, so a login for
// an unknown username takes as long as one with a wrong password.
func DummyVerify() {
	dummyOnce.Do(func() { dummyHash, _ = HashPassword("luma-dummy-password") })
	_, _ = VerifyPassword(dummyHash, "not-the-password")
}
