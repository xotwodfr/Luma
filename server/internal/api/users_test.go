package api_test

import (
	"net/url"
	"testing"
)

func TestUsers_LookupNormalises(t *testing.T) {
	h := newHarness(t)
	a := h.register("alice")
	b := h.register("bob")
	r := h.do("GET", "/v1/users/"+url.PathEscape(" Alice "), b.Token, nil)
	if r.Status != 200 || r.Body["id"] != a.ID || r.Body["username"] != "alice" {
		t.Fatalf("lookup: %d %s", r.Status, r.Raw)
	}
	if _, leaked := r.Body["password_hash"]; leaked {
		t.Fatal("password hash leaked")
	}
}

func TestUsers_LookupRequiresAuth(t *testing.T) {
	h := newHarness(t)
	h.register("alice")
	if r := h.do("GET", "/v1/users/alice", "", nil); r.Status != 401 {
		t.Fatalf("anonymous lookup: %d", r.Status)
	}
}

func TestUsers_LookupHiddenFromBlocked(t *testing.T) {
	h := newHarness(t)
	a := h.register("alice")
	b := h.register("bob")
	if r := h.do("POST", "/v1/users/alice/block", b.Token, nil); r.Status != 204 {
		t.Fatalf("block: %d %s", r.Status, r.Raw)
	}
	hidden := h.do("GET", "/v1/users/bob", a.Token, nil)
	missing := h.do("GET", "/v1/users/nobody_xyz", a.Token, nil)
	if hidden.Status != 404 || string(hidden.Raw) != string(missing.Raw) {
		t.Fatalf("blocked lookup must equal not-found:\n%d %s\n%d %s", hidden.Status, hidden.Raw, missing.Status, missing.Raw)
	}
	// The blocker can still see the person they blocked, so they can unblock.
	r := h.do("GET", "/v1/users/alice", b.Token, nil)
	if r.Status != 200 || r.Body["blocked"] != true {
		t.Fatalf("blocker view: %d %s", r.Status, r.Raw)
	}
	if r := h.do("DELETE", "/v1/users/alice/block", b.Token, nil); r.Status != 204 {
		t.Fatalf("unblock: %d", r.Status)
	}
	if r := h.do("GET", "/v1/users/bob", a.Token, nil); r.Status != 200 {
		t.Fatalf("after unblock: %d", r.Status)
	}
}

func TestUsers_BlockEdgeCases(t *testing.T) {
	h := newHarness(t)
	a := h.register("alice")
	if r := h.do("POST", "/v1/users/alice/block", a.Token, nil); r.Status != 400 {
		t.Fatalf("self block: %d", r.Status)
	}
	if r := h.do("POST", "/v1/users/nobody_xyz/block", a.Token, nil); r.Status != 204 {
		t.Fatalf("block unknown must look like success (no existence leak): %d", r.Status)
	}
	h.register("bob")
	for i := 0; i < 2; i++ {
		if r := h.do("POST", "/v1/users/bob/block", a.Token, nil); r.Status != 204 {
			t.Fatalf("block twice (idempotent) #%d: %d", i, r.Status)
		}
	}
}

func TestMe_Update(t *testing.T) {
	h := newHarness(t)
	a := h.register("alice")
	r := h.do("PATCH", "/v1/me", a.Token, map[string]any{"display_name": "Alice Liddell"})
	if r.Status != 200 || r.Body["display_name"] != "Alice Liddell" {
		t.Fatalf("update: %d %s", r.Status, r.Raw)
	}
	for _, bad := range []string{"", "   ", string(make([]rune, 65))} {
		if r := h.do("PATCH", "/v1/me", a.Token, map[string]any{"display_name": bad}); r.Status != 400 {
			t.Errorf("display name %q accepted: %d", bad, r.Status)
		}
	}
}

// Review finding #7: you can block someone who blocked you first.
func TestUsers_BlockBack(t *testing.T) {
	h := newHarness(t)
	a := h.register("alice")
	b := h.register("bob")
	h.do("POST", "/v1/users/alice/block", b.Token, nil)
	if r := h.do("POST", "/v1/users/bob/block", a.Token, nil); r.Status != 204 {
		t.Fatalf("block back: %d", r.Status)
	}
	h.do("DELETE", "/v1/users/alice/block", b.Token, nil)
	if r := h.do("GET", "/v1/users/alice", b.Token, nil); r.Status != 404 {
		t.Fatalf("alice's own block must still hide her from bob: %d", r.Status)
	}
}
