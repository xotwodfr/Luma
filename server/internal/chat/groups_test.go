package chat_test

import (
	"encoding/json"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"luma.folk3s.com/server/internal/chat"
)

func (w *world) group(owner uuid.UUID, members ...string) uuid.UUID {
	w.t.Helper()
	c, err := w.svc.CreateGroup(w.ctx, owner, "Group", members)
	if err != nil {
		w.t.Fatal(err)
	}
	return c.ID
}

// seqs returns the seqs user can page through in conv (all of them, ascending).
func (w *world) seqs(user, conv uuid.UUID) []int64 {
	w.t.Helper()
	msgs, err := w.svc.History(w.ctx, user, conv, 0, 0, 100)
	if err != nil {
		w.t.Fatal(err)
	}
	out := []int64{}
	for _, m := range msgs {
		out = append(out, m.Seq)
	}
	return out
}

func (w *world) role(conv, user uuid.UUID) string {
	var r string
	w.pool.QueryRow(w.ctx, `SELECT CASE WHEN left_at IS NULL THEN role ELSE 'left' END FROM members WHERE conversation_id = $1 AND user_id = $2`, conv, user).Scan(&r)
	return r
}

func (w *world) userID(name string) uuid.UUID {
	var id uuid.UUID
	w.pool.QueryRow(w.ctx, `SELECT id FROM users WHERE username = $1`, name).Scan(&id)
	return id
}

// eventsFor is user's whole durable event log, i.e. exactly what a reconnecting client replays.
func (w *world) eventsFor(user uuid.UUID) []chat.Event {
	w.t.Helper()
	ev, err := w.svc.EventsAfter(w.ctx, user, 0, 10000)
	if err != nil {
		w.t.Fatal(err)
	}
	return ev
}

func bodyOf(e chat.Event) string {
	var p struct{ Message chat.Message }
	_ = json.Unmarshal(e.Payload, &p)
	return p.Message.Body
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func wantCode(t *testing.T, err error, c string) {
	t.Helper()
	if code(err) != c {
		t.Fatalf("got %v, want %s", err, c)
	}
}

func TestGroups_VisibilityWindows(t *testing.T) {
	w := newWorld(t)
	alice, bob, carol := w.user("alice"), w.user("bob"), w.user("carol")
	g := w.group(alice, "bob")
	w.send(alice, g, "1 before carol")
	_, err := w.svc.AddMembers(w.ctx, bob, g, []string{"carol"}) // any member may add
	must(t, err)
	w.send(bob, g, "3 with carol")
	if got := w.seqs(carol, g); !slices.Equal(got, []int64{2, 3}) {
		t.Fatalf("carol sees %v, want the add notice and later only", got)
	}
	_, err = w.svc.RemoveMember(w.ctx, alice, g, carol)
	must(t, err)
	w.send(alice, g, "5 after carol")
	if got := w.seqs(carol, g); !slices.Equal(got, []int64{2, 3, 4}) {
		t.Fatalf("removed carol sees %v, want up to and including her removal", got)
	}
	c, err := w.svc.Get(w.ctx, carol, g)
	must(t, err)
	if !c.Left || c.LastSeq != 4 || len(c.Members) != 0 {
		t.Fatalf("former member view: left=%v last_seq=%d members=%d", c.Left, c.LastSeq, len(c.Members))
	}
	// Review finding: nothing newer than her removal may show through the conversation itself.
	at := c.LastActivityAt
	_, err = w.svc.Rename(w.ctx, alice, g, "Plans after carol left")
	must(t, err)
	w.send(alice, g, "6 even later")
	c, _ = w.svc.Get(w.ctx, carol, g)
	if *c.Title != "Group" || !c.LastActivityAt.Equal(at) {
		t.Fatalf("former member sees title %q, activity %v (was %v)", *c.Title, c.LastActivityAt, at)
	}
	list, _ := w.svc.List(w.ctx, carol)
	if *list[0].Title != "Group" || !list[0].LastActivityAt.Equal(at) {
		t.Fatalf("list leaks: %q %v", *list[0].Title, list[0].LastActivityAt)
	}
	if _, err := w.svc.History(w.ctx, carol, g, 0, 4, 100); err != nil {
		t.Fatal(err)
	}
	if msgs, _ := w.svc.History(w.ctx, carol, g, 0, 4, 100); len(msgs) != 0 {
		t.Fatalf("after_seq past the window leaked %d messages", len(msgs))
	}
	if got := w.seqs(bob, g); len(got) != 7 {
		t.Fatalf("bob sees %v", got)
	}
}

func TestGroups_RejoinWindowExcludesGap(t *testing.T) {
	w := newWorld(t)
	alice, _, carol := w.user("alice"), w.user("bob"), w.user("carol")
	g := w.group(alice, "bob", "carol")
	w.send(alice, g, "1 everyone")
	_, err := w.svc.RemoveMember(w.ctx, alice, g, carol) // seq 2
	must(t, err)
	w.send(alice, g, "3 secret while carol is away")
	w.send(alice, g, "4 secret while carol is away")
	_, err = w.svc.AddMembers(w.ctx, alice, g, []string{"carol"}) // seq 5
	must(t, err)
	w.send(alice, g, "6 welcome back")
	got := w.seqs(carol, g)
	if !slices.Equal(got, []int64{5, 6}) {
		t.Fatalf("rejoined carol sees %v, want [5 6] (a new window; the gap stays hidden)", got)
	}
	if msgs, _ := w.svc.History(w.ctx, carol, g, 5, 0, 100); len(msgs) != 0 {
		t.Fatalf("before_seq paged into the gap: %d messages", len(msgs))
	}
	c, _ := w.svc.Get(w.ctx, carol, g)
	if c.Unread != 2 || c.Left { // the "added" notice and the welcome
		t.Fatalf("rejoined: unread=%d left=%v", c.Unread, c.Left)
	}
	// Carol's event log never got the gap's messages.
	for _, e := range w.eventsFor(carol) {
		if e.Type == "message.created" && (bodyOf(e) == "3 secret while carol is away" || bodyOf(e) == "4 secret while carol is away") {
			t.Fatal("gap message reached carol's event log")
		}
	}
}

func TestGroups_RolePermissions(t *testing.T) {
	w := newWorld(t)
	alice, bob, carol, dave := w.user("alice"), w.user("bob"), w.user("carol"), w.user("dave")
	w.user("erin")
	g := w.group(alice, "bob", "carol", "dave")

	// Plain member: may add, nothing else.
	_, err := w.svc.Rename(w.ctx, carol, g, "nope")
	wantCode(t, err, "forbidden")
	_, err = w.svc.RemoveMember(w.ctx, carol, g, dave)
	wantCode(t, err, "forbidden")
	_, err = w.svc.SetRole(w.ctx, carol, g, dave, "admin")
	wantCode(t, err, "forbidden")
	_, err = w.svc.AddMembers(w.ctx, carol, g, []string{"erin"})
	must(t, err)

	// Owner promotes bob; admin bob may rename, promote, demote and remove non-owners.
	_, err = w.svc.SetRole(w.ctx, alice, g, bob, "admin")
	must(t, err)
	_, err = w.svc.Rename(w.ctx, bob, g, "  Renamed  ")
	must(t, err)
	c, _ := w.svc.Get(w.ctx, alice, g)
	if *c.Title != "Renamed" {
		t.Fatalf("title %q", *c.Title)
	}
	_, err = w.svc.SetRole(w.ctx, bob, g, carol, "admin")
	must(t, err)
	_, err = w.svc.SetRole(w.ctx, bob, g, carol, "member")
	must(t, err)
	_, err = w.svc.RemoveMember(w.ctx, bob, g, dave)
	must(t, err)

	// Nobody touches the owner; only the owner hands ownership over.
	_, err = w.svc.RemoveMember(w.ctx, bob, g, alice)
	wantCode(t, err, "forbidden")
	_, err = w.svc.SetRole(w.ctx, bob, g, alice, "member")
	wantCode(t, err, "forbidden")
	_, err = w.svc.SetRole(w.ctx, bob, g, carol, "owner")
	wantCode(t, err, "forbidden")
	_, err = w.svc.SetRole(w.ctx, alice, g, carol, "superuser")
	wantCode(t, err, "invalid_role")
	_, err = w.svc.RemoveMember(w.ctx, alice, g, alice)
	wantCode(t, err, "invalid_request")

	// Former members and outsiders are not found, not forbidden.
	_, err = w.svc.Rename(w.ctx, dave, g, "x")
	wantCode(t, err, "not_found")
	_, err = w.svc.RemoveMember(w.ctx, alice, g, dave)
	wantCode(t, err, "not_found")

	_, err = w.svc.SetRole(w.ctx, alice, g, carol, "owner")
	must(t, err)
	if w.role(g, carol) != "owner" || w.role(g, alice) != "admin" {
		t.Fatalf("transfer: carol=%s alice=%s", w.role(g, carol), w.role(g, alice))
	}

	_, err = w.svc.Rename(w.ctx, alice, g, string(make([]rune, 81)))
	wantCode(t, err, "invalid_title")
	d := w.dm(alice, "bob")
	_, err = w.svc.Rename(w.ctx, alice, d, "x")
	wantCode(t, err, "not_a_group")
	wantCode(t, w.svc.Leave(w.ctx, alice, d), "not_a_group")
}

func TestGroups_OwnerLeaveTransfers(t *testing.T) {
	w := newWorld(t)
	alice, bob, carol, dave := w.user("alice"), w.user("bob"), w.user("carol"), w.user("dave")
	g := w.group(alice, "bob", "carol")
	_, err := w.svc.AddMembers(w.ctx, alice, g, []string{"dave"})
	must(t, err)
	_, err = w.svc.SetRole(w.ctx, alice, g, dave, "admin")
	must(t, err)
	must(t, w.svc.Leave(w.ctx, alice, g))
	if w.role(g, dave) != "owner" {
		t.Fatalf("an admin (dave) should inherit before older members; dave=%s", w.role(g, dave))
	}
	must(t, w.svc.Leave(w.ctx, dave, g))
	if w.role(g, bob) != "owner" && w.role(g, carol) != "owner" {
		t.Fatal("no admin left: the longest-standing member should inherit")
	}
	must(t, w.svc.Leave(w.ctx, bob, g))
	must(t, w.svc.Leave(w.ctx, carol, g)) // last one out: no heir, no error
	wantCode(t, w.svc.Leave(w.ctx, carol, g), "not_found")
	msgs, _ := w.svc.History(w.ctx, alice, g, 0, 0, 100)
	if len(msgs) == 0 || msgs[len(msgs)-1].Kind != "system" {
		t.Fatal("alice should still see her own leave notice")
	}
}

func TestGroups_BlockedAddsRefused(t *testing.T) {
	w := newWorld(t)
	alice, bob, _, dave := w.user("alice"), w.user("bob"), w.user("carol"), w.user("dave")
	w.block(alice, dave) // alice blocked dave
	w.block(dave, bob)   // dave blocked bob
	_, err := w.svc.CreateGroup(w.ctx, alice, "G", []string{"bob", "dave"})
	wantCode(t, err, "not_found")
	g := w.group(alice, "bob", "carol")
	_, err = w.svc.AddMembers(w.ctx, alice, g, []string{"dave"})
	wantCode(t, err, "not_found")
	_, err = w.svc.AddMembers(w.ctx, bob, g, []string{"dave"})
	wantCode(t, err, "not_found")
	_, err = w.svc.AddMembers(w.ctx, bob, g, []string{"nobody_here"})
	wantCode(t, err, "not_found") // indistinguishable from a block
	// carol has no block with dave, so she may add him; the group then contains a blocked pair,
	// and messages still flow (collapsing them is the client's job).
	carol := w.userID("carol")
	_, err = w.svc.AddMembers(w.ctx, carol, g, []string{"dave"})
	must(t, err)
	w.send(dave, g, "hello all")
}

func TestGroups_MemberCap(t *testing.T) {
	w := newWorld(t)
	owner := w.user("owner")
	names := make([]string, 0, chat.MaxGroupMembers)
	for i := 0; i < chat.MaxGroupMembers; i++ {
		names = append(names, fmt.Sprintf("u%03d", i))
	}
	_, err := w.pool.Exec(w.ctx, `INSERT INTO users (username, display_name, password_hash) SELECT n, n, 'x' FROM unnest($1::text[]) n`, names)
	must(t, err)
	_, err = w.svc.CreateGroup(w.ctx, owner, "Big", names)
	wantCode(t, err, "group_full")
	g, err := w.svc.CreateGroup(w.ctx, owner, "Big", names[:255])
	must(t, err)
	if len(g.Members) != 256 {
		t.Fatalf("members %d", len(g.Members))
	}
	_, err = w.svc.AddMembers(w.ctx, owner, g.ID, names[255:])
	wantCode(t, err, "group_full")
}

func TestGroups_RemovedMemberCannotSend(t *testing.T) {
	w := newWorld(t)
	alice, bob := w.user("alice"), w.user("bob")
	g := w.group(alice, "bob")
	_, err := w.svc.RemoveMember(w.ctx, alice, g, bob)
	must(t, err)
	_, _, err = w.svc.Send(w.ctx, bob, chat.SendInput{ConversationID: g, ClientID: uuid.New(), Kind: "text", Body: "still here?"})
	wantCode(t, err, "not_found")
	_, err = w.svc.AddMembers(w.ctx, bob, g, []string{"alice"})
	wantCode(t, err, "not_found")
}

// Review focus 2: whatever mix of removals, transfers and leaves runs concurrently, a group
// with members ends with exactly one owner, and nobody acts after losing their rights.
func TestGroups_ConcurrentRemovalsKeepOneOwner(t *testing.T) {
	w := newWorld(t)
	alice := w.user("alice")
	for _, n := range []string{"bob", "carol", "dave"} {
		w.user(n)
	}
	for round := 0; round < 15; round++ {
		g := w.group(alice, "bob", "carol", "dave")
		bob, carol, dave := w.userID("bob"), w.userID("carol"), w.userID("dave")
		for _, u := range []uuid.UUID{bob, carol} {
			_, err := w.svc.SetRole(w.ctx, alice, g, u, "admin")
			must(t, err)
		}
		var wg sync.WaitGroup
		results := make([]error, 5)
		ops := []func() error{
			func() error { _, err := w.svc.RemoveMember(w.ctx, bob, g, carol); return err },
			func() error { _, err := w.svc.RemoveMember(w.ctx, carol, g, bob); return err },
			func() error { return w.svc.Leave(w.ctx, alice, g) },
			func() error { _, err := w.svc.SetRole(w.ctx, alice, g, dave, "owner"); return err },
			func() error { _, err := w.svc.SetRole(w.ctx, bob, g, dave, "member"); return err },
		}
		for i, op := range ops {
			wg.Add(1)
			go func() { defer wg.Done(); results[i] = op() }()
		}
		wg.Wait()
		if results[0] == nil && results[1] == nil {
			t.Fatalf("round %d: bob and carol both removed each other", round)
		}
		for i, err := range results {
			if err != nil && code(err) == "" {
				t.Fatalf("round %d op %d: unexpected error %v", round, i, err)
			}
		}
		var active, owners int
		w.pool.QueryRow(w.ctx, `SELECT count(*), count(*) FILTER (WHERE role = 'owner') FROM members
			WHERE conversation_id = $1 AND left_at IS NULL`, g).Scan(&active, &owners)
		if active > 0 && owners != 1 {
			t.Fatalf("round %d: %d active members, %d owners", round, active, owners)
		}
		var seqs, maxSeq, lastSeq int64
		w.pool.QueryRow(w.ctx, `SELECT count(*), max(seq), (SELECT last_seq FROM conversations WHERE id = $1)
			FROM messages WHERE conversation_id = $1`, g).Scan(&seqs, &maxSeq, &lastSeq)
		if seqs != maxSeq || maxSeq != lastSeq {
			t.Fatalf("round %d: seq gap: %d messages, max %d, last_seq %d", round, seqs, maxSeq, lastSeq)
		}
	}
}

func TestGroups_FanOutFollowsMembership(t *testing.T) {
	w := newWorld(t)
	alice, _, carol := w.user("alice"), w.user("bob"), w.user("carol")
	g := w.group(alice, "bob")
	w.send(alice, g, "before")
	_, err := w.svc.AddMembers(w.ctx, alice, g, []string{"carol"})
	must(t, err)
	w.send(alice, g, "while member")
	_, err = w.svc.RemoveMember(w.ctx, alice, g, carol)
	must(t, err)
	w.send(alice, g, "after removal")

	var types, bodies []string
	for _, e := range w.eventsFor(carol) {
		types = append(types, e.Type)
		if e.Type == "message.created" {
			bodies = append(bodies, bodyOf(e))
		}
	}
	// membership.updated precedes the add notice so carol learns of the group first.
	want := []string{"membership.updated", "message.created", "message.created", "membership.updated", "message.created"}
	if !slices.Equal(types, want) {
		t.Fatalf("carol's events %v, want %v", types, want)
	}
	if !slices.Equal(bodies, []string{"", "while member", ""}) {
		t.Fatalf("carol's message bodies %q", bodies)
	}
}

func TestSettings_MuteArchive(t *testing.T) {
	w := newWorld(t)
	alice, bob := w.user("alice"), w.user("bob")
	conv := w.dm(alice, "bob")
	yes, past, future := true, time.Now().Add(-time.Minute), time.Now().Add(time.Hour)
	set := func(raw string, archived *bool) chat.Conversation {
		t.Helper()
		c, err := w.svc.UpdateSettings(w.ctx, alice, conv, chat.SettingsInput{MutedUntil: []byte(raw), Archived: archived})
		must(t, err)
		return c
	}
	if c := set(`"`+future.Format(time.RFC3339)+`"`, nil); !c.Muted || c.MutedUntil == nil {
		t.Fatal("future mute not reflected")
	}
	if c := set(`"`+past.Format(time.RFC3339)+`"`, nil); c.Muted {
		t.Fatal("expired mute still reported as muted")
	}
	if c := set(`null`, &yes); c.Muted || c.MutedUntil != nil || !c.Archived {
		t.Fatalf("unmute+archive: %+v", c)
	}
	list, _ := w.svc.List(w.ctx, alice)
	if len(list) != 1 || !list[0].Archived {
		t.Fatal("archived conversation must stay in the list, flagged")
	}
	if c := set(``, nil); !c.Archived { // absent fields change nothing
		t.Fatal("empty update changed archived")
	}
	w.send(bob, conv, "wake up")
	if c, _ := w.svc.Get(w.ctx, alice, conv); c.Archived {
		t.Fatal("a new message should bring the conversation back to the inbox")
	}
	_, err := w.svc.UpdateSettings(w.ctx, alice, conv, chat.SettingsInput{MutedUntil: []byte(`"tomorrow"`)})
	wantCode(t, err, "invalid_request")
	_, err = w.svc.UpdateSettings(w.ctx, w.user("carol"), conv, chat.SettingsInput{Archived: &yes})
	wantCode(t, err, "not_found")
	var sawSettings bool
	for _, e := range w.eventsFor(alice) {
		sawSettings = sawSettings || e.Type == "settings.updated"
	}
	if !sawSettings {
		t.Fatal("no settings.updated event for alice's other devices")
	}
}

// A send racing the sender's removal either lands inside their window or is refused; it
// never lands after their left_seq (the stale-snapshot bug fixed by lockConversation).
func TestSend_RacingRemovalStaysInWindow(t *testing.T) {
	w := newWorld(t)
	alice, bob := w.user("alice"), w.user("bob")
	for round := 0; round < 30; round++ {
		g := w.group(alice, "bob")
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			for i := 0; i < 5; i++ {
				w.svc.Send(w.ctx, bob, chat.SendInput{ConversationID: g, ClientID: uuid.New(), Kind: "text", Body: "x"})
			}
		}()
		go func() { defer wg.Done(); w.svc.RemoveMember(w.ctx, alice, g, bob) }()
		wg.Wait()
		var late int
		w.pool.QueryRow(w.ctx, `SELECT count(*) FROM messages msg JOIN members m ON m.conversation_id = msg.conversation_id AND m.user_id = msg.sender_id
			WHERE msg.conversation_id = $1 AND msg.sender_id = $2 AND msg.seq > m.left_seq`, g, bob).Scan(&late)
		if late > 0 {
			t.Fatalf("round %d: %d messages from bob landed after his removal", round, late)
		}
	}
}
