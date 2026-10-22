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
	for _, n := range []string{"alice", "bob", "carol", "mallory"} {
		tokens[n] = h.register(n).Token
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
	parts := strings.Split(p, "/")
	for i, s := range parts {
		if strings.HasPrefix(s, "{") || (len(s) == 36 && strings.Count(s, "-") == 4) || s == "not-a-uuid" {
			parts[i] = "{id}"
		}
		if i == 3 && parts[2] == "users" {
			parts[i] = "{username}"
		}
	}
	return strings.Join(parts, "/")
}

func TestSend_RateLimited(t *testing.T) {
	h := newHarness(t, productionLimits)
	a := h.register("alice")
	h.register("bob")
	conv := h.do("POST", "/v1/conversations", a.Token, map[string]any{"kind": "direct", "username": "bob"}).Body["id"].(string)
	var last resp
	for i := 0; i < 31; i++ {
		last = h.do("POST", "/v1/conversations/"+conv+"/messages", a.Token, map[string]any{"client_id": uuid.NewString(), "kind": "text", "body": "x"})
	}
	if last.Status != 429 || last.Header.Get("Retry-After") == "" {
		t.Fatalf("31st message in 10s: %d", last.Status)
	}
}
