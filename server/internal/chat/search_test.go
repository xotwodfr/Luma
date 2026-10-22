package chat_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func (w *world) search(user uuid.UUID, q string, conv *uuid.UUID) []string {
	w.t.Helper()
	res, err := w.svc.Search(w.ctx, user, q, conv, 50)
	if err != nil {
		w.t.Fatal(err)
	}
	out := []string{}
	for _, r := range res {
		out = append(out, r.Message.Body)
	}
	return out
}

func TestSearch_RespectsVisibilityWindows(t *testing.T) {
	w := newWorld(t)
	alice, _, carol, mallory := w.user("alice"), w.user("bob"), w.user("carol"), w.user("mallory")
	g := w.group(alice, "bob", "carol")
	w.send(alice, g, "apple one")
	_, err := w.svc.RemoveMember(w.ctx, alice, g, carol)
	must(t, err)
	w.send(alice, g, "apple two while carol is out")
	if got := w.search(carol, "apple", nil); !slices.Equal(got, []string{"apple one"}) {
		t.Fatalf("removed carol finds %q", got)
	}
	_, err = w.svc.AddMembers(w.ctx, alice, g, []string{"carol"})
	must(t, err)
	w.send(alice, g, "apple three")
	if got := w.search(carol, "apple", nil); !slices.Equal(got, []string{"apple three"}) {
		t.Fatalf("rejoined carol finds %q (gap and old window must stay hidden)", got)
	}
	if got := w.search(alice, "apple", nil); !slices.Equal(got, []string{"apple three", "apple two while carol is out", "apple one"}) {
		t.Fatalf("alice finds %q, want newest first", got)
	}
	if got := w.search(mallory, "apple", nil); len(got) != 0 {
		t.Fatalf("non-member finds %q", got)
	}
	dm := w.dm(alice, "mallory")
	w.send(mallory, dm, "apple pie")
	if got := w.search(alice, "apple", &dm); !slices.Equal(got, []string{"apple pie"}) {
		t.Fatalf("conversation filter: %q", got)
	}
	if got := w.search(alice, `"apple two" -carol`, nil); len(got) != 0 {
		t.Fatalf("websearch syntax: %q", got)
	}
	if got := w.search(alice, `"apple two"`, nil); len(got) != 1 {
		t.Fatalf("phrase: %q", got)
	}
	if got := w.search(alice, "&&& !!", nil); len(got) != 0 { // reduces to an empty query: no error, no hits
		t.Fatalf("punctuation query: %q", got)
	}
}

func TestSearch_HeadlineMarkersAreServers(t *testing.T) {
	w := newWorld(t)
	alice := w.user("alice")
	w.user("bob")
	dm := w.dm(alice, "bob")
	w.send(alice, dm, "fake \x01bold\x02 then the real banana")
	res, err := w.svc.Search(w.ctx, alice, "banana", nil, 10)
	must(t, err)
	if len(res) != 1 {
		t.Fatalf("results %d", len(res))
	}
	h := res[0].Headline
	if strings.Count(h, "\x01") != 1 || !strings.Contains(h, "\x01banana\x02") || strings.Contains(h, "<b>") {
		t.Fatalf("headline %q", h)
	}
}

// Documented choice: in a shared group, a blocked person's messages are still searchable;
// hiding them is a client-side collapse, the same as in the timeline.
func TestSearch_BlockedSendersNotFiltered(t *testing.T) {
	w := newWorld(t)
	alice, _, dave := w.user("alice"), w.user("bob"), w.user("dave")
	g := w.group(alice, "bob", "dave")
	w.block(alice, dave)
	w.send(dave, g, "cherry from dave")
	if got := w.search(alice, "cherry", nil); len(got) != 1 {
		t.Fatalf("got %q", got)
	}
}

func TestSearch_Validation(t *testing.T) {
	w := newWorld(t)
	alice := w.user("alice")
	for _, c := range []struct {
		q     string
		limit int
	}{{"", 10}, {"   ", 10}, {strings.Repeat("a", 257), 10}, {"ok", 0}, {"ok", 51}} {
		_, err := w.svc.Search(w.ctx, alice, c.q, nil, c.limit)
		wantCode(t, err, "invalid_request")
	}
}
