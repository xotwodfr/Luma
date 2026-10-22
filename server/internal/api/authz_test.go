package api_test

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
)

type matrixRow struct {
	Method string         `json:"method"`
	Path   string         `json:"path"`
	As     string         `json:"as"`
	Body   map[string]any `json:"body"`
	Expect int            `json:"expect"`
}

// authzWorld builds the fixed world the matrix is written against.
func authzWorld(t *testing.T) (*harness, map[string]string, map[string]string) {
	h := newHarness(t)
	tokens := map[string]string{"anon": "", "bogus": "not-a-real-session-token"}
	ids := map[string]string{}
	for _, n := range []string{"alice", "bob", "carol", "mallory", "dave"} {
		u := h.register(n)
		tokens[n], ids[n] = u.Token, u.ID
	}
	open := func(as, with string) string {
		r := h.do("POST", "/v1/conversations", tokens[as], map[string]any{"kind": "direct", "username": with})
		if r.Status != 200 {
			t.Fatalf("open %s→%s: %d %s", as, with, r.Status, r.Raw)
		}
		return r.Body["id"].(string)
	}
	vars := map[string]string{"ab": open("alice", "bob"), "am": open("alice", "mallory")}
	h.do("POST", "/v1/conversations/"+vars["am"]+"/messages", tokens["mallory"], map[string]any{"client_id": uuid.NewString(), "kind": "text", "body": "before the block"})
	if r := h.do("POST", "/v1/users/mallory/block", tokens["alice"], nil); r.Status != 204 {
		t.Fatalf("block: %d", r.Status)
	}
	// Group {g}: alice owner, bob admin, carol member, dave former member (removed after one message).
	g := h.do("POST", "/v1/conversations", tokens["alice"], map[string]any{"kind": "group", "title": "G", "usernames": []string{"bob", "carol", "dave"}})
	if g.Status != 201 {
		t.Fatalf("create group: %d %s", g.Status, g.Raw)
	}
	vars["g"] = g.Body["id"].(string)
	for k, v := range ids {
		vars[k+"ID"] = v
	}
	h.do("PATCH", "/v1/conversations/"+vars["g"]+"/members/"+ids["bob"], tokens["alice"], map[string]any{"role": "admin"})
	h.do("POST", "/v1/conversations/"+vars["g"]+"/messages", tokens["alice"], map[string]any{"client_id": uuid.NewString(), "kind": "text", "body": "dave can see this"})
	vars["gOld"] = h.mustUpload(user{Token: tokens["alice"]}, vars["g"], "old.txt", []byte("old"))
	h.sendFile(user{Token: tokens["alice"]}, vars["g"], vars["gOld"])
	if r := h.do("DELETE", "/v1/conversations/"+vars["g"]+"/members/"+ids["dave"], tokens["alice"], nil); r.Status != 200 {
		t.Fatalf("remove dave: %d %s", r.Status, r.Raw)
	}
	// Files: {f} attached in {ab}; {gOld} in {g} before dave's removal, {gNew} after; {draft} unattached (alice's).
	alice := user{ID: ids["alice"], Token: tokens["alice"]}
	vars["f"] = h.mustUpload(alice, vars["ab"], "f.txt", []byte("f"))
	h.sendFile(alice, vars["ab"], vars["f"])
	vars["gNew"] = h.mustUpload(alice, vars["g"], "new.txt", []byte("new"))
	h.sendFile(alice, vars["g"], vars["gNew"])
	vars["draft"] = h.mustUpload(alice, vars["g"], "draft.txt", []byte("draft"))
	sessions := h.do("GET", "/v1/auth/sessions", tokens["alice"], nil).Body["sessions"].([]any)
	vars["aliceSession"] = sessions[0].(map[string]any)["id"].(string)
	return h, tokens, vars
}

func TestAuthzMatrix(t *testing.T) {
	raw, err := os.ReadFile("authz_matrix.json")
	if err != nil {
		t.Fatal(err)
	}
	var m struct{ Rows []matrixRow }
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	h, tokens, vars := authzWorld(t)
	subst := func(s string) string {
		for k, v := range vars {
			s = strings.ReplaceAll(s, "{"+k+"}", v)
		}
		return strings.ReplaceAll(s, "{uuid}", uuid.NewString())
	}
	for _, row := range m.Rows {
		var body any
		if row.Body != nil {
			b, _ := json.Marshal(row.Body)
			var v map[string]any
			_ = json.Unmarshal([]byte(subst(string(b))), &v)
			body = v
		}
		name := fmt.Sprintf("%s %s as %s", row.Method, row.Path, row.As)
		tok, ok := tokens[row.As]
		if !ok {
			t.Fatalf("%s: unknown principal", name)
		}
		r := h.do(row.Method, subst(row.Path), tok, body)
		if r.Status != row.Expect {
			t.Errorf("%s: got %d, want %d (%s)", name, r.Status, row.Expect, r.Raw)
		}
	}
}

// Every registered route must be covered by at least one matrix row.
func TestAuthzMatrix_CoversAllRoutes(t *testing.T) {
	raw, _ := os.ReadFile("authz_matrix.json")
	var m struct{ Rows []matrixRow }
	_ = json.Unmarshal(raw, &m)
	covered := map[string]bool{}
	for _, r := range m.Rows {
		covered[r.Method+" "+normalisePath(r.Path)] = true
	}
	h := newHarness(t)
	for _, route := range h.app.Routes() {
		if !covered[route] {
			t.Errorf("route %q has no authorization matrix row", route)
		}
	}
}

// normalisePath turns concrete matrix paths back into route patterns.
func normalisePath(p string) string {
	p, _, _ = strings.Cut(p, "?")
	parts := strings.Split(p, "/")
	for i, s := range parts {
		if strings.HasPrefix(s, "{") || (len(s) == 36 && strings.Count(s, "-") == 4) || s == "not-a-uuid" {
			parts[i] = "{id}"
		}
		if i == 3 && parts[2] == "users" {
			parts[i] = "{username}"
		}
		if i == 5 && parts[4] == "members" {
			parts[i] = "{user_id}"
		}
	}
	return strings.Join(parts, "/")
}

func TestSend_RateLimited(t *testing.T) {
	h := newHarness(t, productionLimits)
	a := h.register("alice")
	h.register("bob")
	conv := h.do("POST", "/v1/conversations", a.Token, map[string]any{"kind": "direct", "username": "bob"}).Body["id"].(string)
	// The bucket refills continuously (one token per 333 ms), so assert the property, not exact timing.
	limited := 0
	for i := 0; i < 45; i++ {
		r := h.do("POST", "/v1/conversations/"+conv+"/messages", a.Token, map[string]any{"client_id": uuid.NewString(), "kind": "text", "body": "x"})
		if r.Status == 429 {
			if r.Header.Get("Retry-After") == "" {
				t.Fatal("429 without Retry-After")
			}
			limited++
		}
	}
	if limited == 0 {
		t.Fatal("45 rapid sends were never rate limited")
	}
}

// Review finding #4: only genuinely new DMs count against the 20/day bucket.
func TestOpenDirect_LimitOnlyCountsNew(t *testing.T) {
	h := newHarness(t) // the 20/day DM limit is always the production value
	a := h.register("alice")
	h.register("bob")
	for i := 0; i < 25; i++ {
		if r := h.do("POST", "/v1/conversations", a.Token, map[string]any{"kind": "direct", "username": "bob"}); r.Status != 200 {
			t.Fatalf("re-opening an existing DM #%d: %d", i, r.Status)
		}
	}
	for i := 0; i < 20; i++ {
		h.register(fmt.Sprintf("friend_%d", i))
	}
	for i := 0; i < 19; i++ { // bob was the first new DM
		if r := h.do("POST", "/v1/conversations", a.Token, map[string]any{"kind": "direct", "username": fmt.Sprintf("friend_%d", i)}); r.Status != 200 {
			t.Fatalf("new DM %d: %d %s", i, r.Status, r.Raw)
		}
	}
	r := h.do("POST", "/v1/conversations", a.Token, map[string]any{"kind": "direct", "username": "friend_19"})
	if r.Status != 429 || r.Header.Get("Retry-After") == "" {
		t.Fatalf("21st new DM in a day: %d", r.Status)
	}
}
