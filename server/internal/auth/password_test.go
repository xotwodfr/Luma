package auth

import (
	"strings"
	"testing"
)

func TestPassword_RoundTrip(t *testing.T) {
	phc, err := HashPassword("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(phc, "$argon2id$v=19$m=65536,t=3,p=2$") {
		t.Fatalf("unexpected PHC prefix: %s", phc)
	}
	ok, err := VerifyPassword(phc, "correct horse battery")
	if err != nil || !ok {
		t.Fatalf("verify correct: ok=%v err=%v", ok, err)
	}
	ok, err = VerifyPassword(phc, "correct horse batterY")
	if err != nil || ok {
		t.Fatalf("verify wrong: ok=%v err=%v", ok, err)
	}
	if _, err := VerifyPassword("$argon2id$garbage", "x"); err == nil {
		t.Fatal("malformed PHC must error")
	}
}

func TestToken_HashStable(t *testing.T) {
	plain, hash := NewToken()
	if len(plain) != 43 { // 32 bytes, base64url without padding
		t.Fatalf("token length %d", len(plain))
	}
	if string(HashToken(plain)) != string(hash) {
		t.Fatal("HashToken(plain) != hash")
	}
	other, _ := NewToken()
	if other == plain {
		t.Fatal("tokens must be random")
	}
}

func TestInviteCode_Normalise(t *testing.T) {
	code := newInviteCode()
	if len(code) != 19 || strings.Count(code, "-") != 3 {
		t.Fatalf("invite format: %q", code)
	}
	if string(hashInvite(strings.ToLower(" "+strings.ReplaceAll(code, "-", "")+" "))) != string(hashInvite(code)) {
		t.Fatal("invite hash must ignore case, dashes and surrounding space")
	}
}
