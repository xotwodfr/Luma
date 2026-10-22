package api_test

import (
	"context"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"

	"luma.folk3s.com/server/internal/auth"
	"luma.folk3s.com/server/internal/db"
)

func TestRegister_RequiresValidUnusedInvite(t *testing.T) {
	h := newHarness(t)
	code := h.invite()
	body := func(u string) map[string]any {
		return map[string]any{"invite": code, "username": u, "display_name": "A", "password": "long-enough-pw"}
	}
	if r := h.do("POST", "/v1/auth/register", "", body("first")); r.Status != 201 {
		t.Fatalf("first use: %d %s", r.Status, r.Raw)
	}
	if r := h.do("POST", "/v1/auth/register", "", body("second")); r.Status != 400 || r.errCode() != "invalid_invite" {
		t.Fatalf("reuse: %d %s", r.Status, r.errCode())
	}
	r := h.do("POST", "/v1/auth/register", "", map[string]any{"invite": "AAAA-BBBB-CCCC-DDDD", "username": "third", "display_name": "A", "password": "long-enough-pw"})
	if r.Status != 400 || r.errCode() != "invalid_invite" {
		t.Fatalf("bogus invite: %d %s", r.Status, r.errCode())
	}
}

func TestRegister_UsernameRules(t *testing.T) {
	h := newHarness(t)
	for _, bad := range []string{"Al", "alice!", " alice", "alice ", strings.Repeat("a", 25)} {
		r := h.do("POST", "/v1/auth/register", "", map[string]any{"invite": h.invite(), "username": bad, "display_name": "A", "password": "long-enough-pw"})
		if r.Status != 400 || r.errCode() != "invalid_username" {
			t.Errorf("%q: %d %s", bad, r.Status, r.errCode())
		}
	}
	u := h.register("Alice")
	if u.Username != "alice" {
		t.Fatalf("stored username %q, want alice", u.Username)
	}
	r := h.do("POST", "/v1/auth/register", "", map[string]any{"invite": h.invite(), "username": "ALICE", "display_name": "A", "password": "long-enough-pw"})
	if r.Status != 409 || r.errCode() != "username_unavailable" {
		t.Fatalf("duplicate: %d %s", r.Status, r.errCode())
	}
}

func TestRegister_UsernameTakenOnlyRevealedWithValidInvite(t *testing.T) {
	h := newHarness(t)
	h.register("alice")
	r := h.do("POST", "/v1/auth/register", "", map[string]any{"invite": "AAAA-BBBB-CCCC-DDDD", "username": "alice", "display_name": "A", "password": "long-enough-pw"})
	if r.errCode() != "invalid_invite" {
		t.Fatalf("got %s; a taken username must not be revealed without a valid invite", r.errCode())
	}
}

func TestRegister_PasswordRules(t *testing.T) {
	h := newHarness(t)
	for _, pw := range []string{"short", strings.Repeat("x", 257)} {
		r := h.do("POST", "/v1/auth/register", "", map[string]any{"invite": h.invite(), "username": "bob", "display_name": "B", "password": pw})
		if r.Status != 400 || r.errCode() != "invalid_password" {
			t.Errorf("len %d: %d %s", len(pw), r.Status, r.errCode())
		}
	}
}

func TestLogin_EnumerationSafe(t *testing.T) {
	// Use a realistic Argon2 cost so hashing dominates (as in production) rather than HTTP/DB noise.
	auth.UseParamsForTesting(auth.Params{Memory: 16 * 1024, Time: 2, Threads: 1})
	t.Cleanup(func() { auth.UseParamsForTesting(auth.Params{Memory: 1024, Time: 1, Threads: 1}) })
	h := newHarness(t)
	h.register("alice")
	median := func(d []time.Duration) time.Duration {
		sort.Slice(d, func(i, j int) bool { return d[i] < d[j] })
		return d[len(d)/2]
	}
	var unknown, wrong []time.Duration
	for i := 0; i < 7; i++ {
		start := time.Now()
		a := h.login("nobody_here", "whatever-password")
		unknown = append(unknown, time.Since(start))
		start = time.Now()
		b := h.login("alice", "wrong-password")
		wrong = append(wrong, time.Since(start))
		if a.Status != b.Status || a.errCode() != "invalid_credentials" || string(a.Raw) != string(b.Raw) {
			t.Fatalf("responses differ:\n%d %s\n%d %s", a.Status, a.Raw, b.Status, b.Raw)
		}
	}
	ratio := float64(median(unknown)) / float64(median(wrong))
	if ratio < 0.7 || ratio > 1.3 {
		t.Fatalf("median timing ratio unknown/wrong = %.2f, want within 30%%", ratio)
	}
	if r := h.login("ALICE", "password-alice"); r.Status != 200 {
		t.Fatalf("login with different case: %d", r.Status)
	}
}

func TestLogin_RateLimited(t *testing.T) {
	h := newHarness(t, productionLimits)
	var last resp
	for i := 0; i < 11; i++ {
		last = h.login("nobody_here", "whatever-password")
	}
	if last.Status != http.StatusTooManyRequests || last.Header.Get("Retry-After") == "" {
		t.Fatalf("11th attempt: %d retry-after=%q", last.Status, last.Header.Get("Retry-After"))
	}
}

func TestLogin_ProxyIPOnlyTrustedWhenConfigured(t *testing.T) {
	// TrustedProxy is off in the harness: a spoofed CF-Connecting-IP must not give a fresh bucket.
	h := newHarness(t, productionLimits)
	var last resp
	for i := 0; i < 11; i++ {
		last = h.do("POST", "/v1/auth/login", "", map[string]any{"username": "nobody", "password": "whatever-pw"},
			"CF-Connecting-IP", "203.0.113."+string(rune('1'+i%9)))
	}
	if last.Status != http.StatusTooManyRequests {
		t.Fatalf("spoofed header bypassed rate limit: %d", last.Status)
	}
}

func TestSessions_RevokeAndPasswordChange(t *testing.T) {
	h := newHarness(t)
	a := h.register("alice")
	second := h.login("alice", "password-alice").Body["token"].(string)
	third := h.login("alice", "password-alice").Body["token"].(string)

	list := h.do("GET", "/v1/auth/sessions", a.Token, nil)
	sessions := list.Body["sessions"].([]any)
	if len(sessions) != 3 {
		t.Fatalf("sessions = %d", len(sessions))
	}
	var thirdID string
	for _, s := range sessions {
		m := s.(map[string]any)
		if m["current"] != true && thirdID == "" {
			thirdID = m["id"].(string)
		}
	}
	if r := h.do("DELETE", "/v1/auth/sessions/"+thirdID, a.Token, nil); r.Status != 204 {
		t.Fatalf("revoke: %d %s", r.Status, r.Raw)
	}
	alive := 0
	for _, tok := range []string{second, third} {
		if h.do("GET", "/v1/me", tok, nil).Status == 200 {
			alive++
		}
	}
	if alive != 1 {
		t.Fatalf("after revoking one of two other sessions, %d remain alive", alive)
	}

	r := h.do("POST", "/v1/me/password", a.Token, map[string]any{"current_password": "password-alice", "new_password": "a-new-password"})
	if r.Status != 204 {
		t.Fatalf("change password: %d %s", r.Status, r.Raw)
	}
	if h.do("GET", "/v1/me", a.Token, nil).Status != 200 {
		t.Fatal("current session must survive a password change")
	}
	for _, tok := range []string{second, third} {
		if h.do("GET", "/v1/me", tok, nil).Status != 401 {
			t.Fatal("other sessions must be revoked by a password change")
		}
	}
	if h.login("alice", "password-alice").Status != 401 || h.login("alice", "a-new-password").Status != 200 {
		t.Fatal("old password must stop working, new one must work")
	}
	if r := h.do("POST", "/v1/auth/logout", a.Token, nil); r.Status != 204 {
		t.Fatalf("logout: %d", r.Status)
	}
	if h.do("GET", "/v1/me", a.Token, nil).Status != 401 {
		t.Fatal("token must be dead after logout")
	}
}

func TestSessions_CannotRevokeSomeoneElses(t *testing.T) {
	h := newHarness(t)
	a := h.register("alice")
	b := h.register("bob")
	sessions := h.do("GET", "/v1/auth/sessions", b.Token, nil).Body["sessions"].([]any)
	bobSession := sessions[0].(map[string]any)["id"].(string)
	if r := h.do("DELETE", "/v1/auth/sessions/"+bobSession, a.Token, nil); r.Status != 404 {
		t.Fatalf("revoking another user's session: %d", r.Status)
	}
	if h.do("GET", "/v1/me", b.Token, nil).Status != 200 {
		t.Fatal("bob's session must be untouched")
	}
}

func TestSession_SlidingExpiry(t *testing.T) {
	h := newHarness(t)
	a := h.register("alice")
	if _, err := h.pool.Exec(context.Background(), `UPDATE sessions SET last_seen_at = now() - interval '91 days'`); err != nil {
		t.Fatal(err)
	}
	if r := h.do("GET", "/v1/me", a.Token, nil); r.Status != 401 {
		t.Fatalf("expired session: %d", r.Status)
	}
}

func TestAuth_MalformedTokens(t *testing.T) {
	h := newHarness(t)
	for _, hdr := range []string{"", "Bearer", "Bearer ", "Basic abc", "Bearer not-a-real-token"} {
		req, _ := http.NewRequest("GET", h.srv.URL+"/v1/me", nil)
		if hdr != "" {
			req.Header.Set("Authorization", hdr)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != 401 {
			t.Errorf("%q: %d", hdr, res.StatusCode)
		}
	}
}

func TestDisabledUser_CannotLogin(t *testing.T) {
	h := newHarness(t)
	a := h.register("alice")
	if err := h.app.Auth.DisableUser(context.Background(), "alice"); err != nil {
		t.Fatal(err)
	}
	if h.do("GET", "/v1/me", a.Token, nil).Status != 401 {
		t.Fatal("disabling must revoke existing sessions")
	}
	if r := h.login("alice", "password-alice"); r.Status == 200 {
		t.Fatal("disabled user logged in")
	}
}

func TestInvites_UserQuota(t *testing.T) {
	h := newHarness(t)
	a := h.register("alice")
	var codes []string
	for i := 0; i < 5; i++ {
		r := h.do("POST", "/v1/invites", a.Token, nil)
		if r.Status != 201 {
			t.Fatalf("invite %d: %d %s", i, r.Status, r.Raw)
		}
		codes = append(codes, r.Body["code"].(string))
	}
	if r := h.do("POST", "/v1/invites", a.Token, nil); r.Status != 429 || r.errCode() != "invite_limit" {
		t.Fatalf("6th invite: %d %s", r.Status, r.errCode())
	}
	r := h.do("POST", "/v1/auth/register", "", map[string]any{"invite": codes[0], "username": "bob", "display_name": "Bob", "password": "long-enough-pw"})
	if r.Status != 201 {
		t.Fatalf("register with user invite: %d %s", r.Status, r.Raw)
	}
}

func TestBodyLimits(t *testing.T) {
	h := newHarness(t)
	big := strings.Repeat("x", 140*1024)
	r := h.do("POST", "/v1/auth/login", "", map[string]any{"username": "alice", "password": big})
	if r.Status != 413 && r.Status != 400 {
		t.Fatalf("oversized body: %d", r.Status)
	}
	req, _ := http.NewRequest("POST", h.srv.URL+"/v1/auth/login", strings.NewReader(`{"username":"a","password":"b","extra":1}`))
	res, _ := http.DefaultClient.Do(req)
	res.Body.Close()
	if res.StatusCode != 400 {
		t.Fatalf("unknown field: %d", res.StatusCode)
	}
}

func TestLogs_NoSecrets(t *testing.T) {
	h := newHarness(t)
	code := h.invite()
	r := h.do("POST", "/v1/auth/register", "", map[string]any{"invite": code, "username": "alice", "display_name": "Alice", "password": "super-secret-pw-123"})
	token := r.Body["token"].(string)
	h.login("alice", "super-secret-pw-123")
	h.do("GET", "/v1/me", token, nil)
	out := h.logs.String()
	if out == "" {
		t.Fatal("expected request logs")
	}
	for _, secret := range []string{"super-secret-pw-123", token, code, url.QueryEscape(token)} {
		if strings.Contains(out, secret) {
			t.Fatalf("logs contain a secret: %q", secret)
		}
	}
}

func TestHealth(t *testing.T) {
	h := newHarness(t)
	if r := h.do("GET", "/v1/health/live", "", nil); r.Status != 200 {
		t.Fatalf("live: %d", r.Status)
	}
	r := h.do("GET", "/v1/health/ready", "", nil)
	if r.Status != 200 || r.Body["status"] != "ok" || r.Body["migration"] != float64(db.LatestVersion()) {
		t.Fatalf("ready: %d %s", r.Status, r.Raw)
	}
}
