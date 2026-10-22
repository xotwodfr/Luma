package chat_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"luma.folk3s.com/server/internal/chat"
	"luma.folk3s.com/server/internal/httpx"
	"luma.folk3s.com/server/internal/testdb"
)

type recorder struct {
	mu     sync.Mutex
	events []chat.Event
	calls  int
}

func (r *recorder) Committed(ev []chat.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	r.events = append(r.events, ev...)
}

type world struct {
	t    *testing.T
	pool *pgxpool.Pool
	svc  *chat.Service
	rec  *recorder
	ctx  context.Context
}

func newWorld(t *testing.T) *world {
	pool := testdb.New(t)
	rec := &recorder{}
	return &world{t: t, pool: pool, svc: chat.NewService(pool, rec), rec: rec, ctx: context.Background()}
}

func (w *world) user(name string) uuid.UUID {
	w.t.Helper()
	var id uuid.UUID
	if err := w.pool.QueryRow(w.ctx, `INSERT INTO users (username, display_name, password_hash) VALUES ($1, $2, 'x') RETURNING id`, name, name).Scan(&id); err != nil {
		w.t.Fatal(err)
	}
	return id
}

func (w *world) block(blocker, blocked uuid.UUID) {
	w.t.Helper()
	if _, err := w.pool.Exec(w.ctx, `INSERT INTO blocks VALUES ($1, $2)`, blocker, blocked); err != nil {
		w.t.Fatal(err)
	}
}

func (w *world) dm(a uuid.UUID, b string) uuid.UUID {
	w.t.Helper()
	c, err := w.svc.OpenDirect(w.ctx, a, b)
	if err != nil {
		w.t.Fatal(err)
	}
	return c.ID
}

func (w *world) send(from, conv uuid.UUID, body string) chat.Message {
	w.t.Helper()
	m, _, err := w.svc.Send(w.ctx, from, chat.SendInput{ConversationID: conv, ClientID: uuid.New(), Kind: "text", Body: body})
	if err != nil {
		w.t.Fatal(err)
	}
	return m
}

func code(err error) string {
	var e *httpx.Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

func TestSend_ConcurrentGapFree(t *testing.T) {
	w := newWorld(t)
	alice, bob := w.user("alice"), w.user("bob")
	conv := w.dm(alice, "bob")

	var wg sync.WaitGroup
	errs := make(chan error, 200)
	for g := 0; g < 50; g++ {
		from := alice
		if g%2 == 1 {
			from = bob
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 4; i++ {
				_, _, err := w.svc.Send(w.ctx, from, chat.SendInput{ConversationID: conv, ClientID: uuid.New(), Kind: "text", Body: "hi"})
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var n, min, max, distinct int64
	w.pool.QueryRow(w.ctx, `SELECT count(*), min(seq), max(seq), count(DISTINCT seq) FROM messages WHERE conversation_id = $1`, conv).Scan(&n, &min, &max, &distinct)
	if n != 200 || min != 1 || max != 200 || distinct != 200 {
		t.Fatalf("seqs: n=%d min=%d max=%d distinct=%d", n, min, max, distinct)
	}
	for _, u := range []uuid.UUID{alice, bob} {
		var cnt, mx, last int64
		w.pool.QueryRow(w.ctx, `SELECT count(*), coalesce(max(seq),0) FROM user_events WHERE user_id = $1`, u).Scan(&cnt, &mx)
		w.pool.QueryRow(w.ctx, `SELECT event_seq FROM users WHERE id = $1`, u).Scan(&last)
		if cnt != mx || mx != last || cnt != 201 { // conversation.created + 200 messages
			t.Fatalf("user %s events: count=%d max=%d counter=%d", u, cnt, mx, last)
		}
	}
}

func TestSend_DuplicateClientID(t *testing.T) {
	w := newWorld(t)
	alice := w.user("alice")
	w.user("bob")
	conv := w.dm(alice, "bob")
	in := chat.SendInput{ConversationID: conv, ClientID: uuid.New(), Kind: "text", Body: "once"}

	first, created, err := w.svc.Send(w.ctx, alice, in)
	if err != nil || !created {
		t.Fatalf("first: created=%v err=%v", created, err)
	}
	again, created, err := w.svc.Send(w.ctx, alice, in)
	if err != nil || created || again.ID != first.ID || again.Seq != first.Seq {
		t.Fatalf("sequential retry: created=%v err=%v %v vs %v", created, err, again.Seq, first.Seq)
	}

	in.ClientID = uuid.New()
	var wg sync.WaitGroup
	results := make(chan chat.Message, 10)
	createdCount := 0
	var mu sync.Mutex
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m, c, err := w.svc.Send(w.ctx, alice, in)
			if err != nil {
				t.Error(err)
				return
			}
			mu.Lock()
			if c {
				createdCount++
			}
			mu.Unlock()
			results <- m
		}()
	}
	wg.Wait()
	close(results)
	var seq int64 = -1
	for m := range results {
		if seq == -1 {
			seq = m.Seq
		}
		if m.Seq != seq {
			t.Fatalf("concurrent retries returned different seqs: %d vs %d", m.Seq, seq)
		}
	}
	if createdCount != 1 {
		t.Fatalf("created %d times, want exactly 1", createdCount)
	}
	var last int64
	w.pool.QueryRow(w.ctx, `SELECT last_seq FROM conversations WHERE id = $1`, conv).Scan(&last)
	if last != 2 {
		t.Fatalf("last_seq = %d, retries must not burn seqs", last)
	}
}

func TestSend_RetryAfterLostAck(t *testing.T) {
	w := newWorld(t)
	alice := w.user("alice")
	w.user("bob")
	conv := w.dm(alice, "bob")
	in := chat.SendInput{ConversationID: conv, ClientID: uuid.New(), Kind: "text", Body: "did this arrive?"}
	orig, _, _ := w.svc.Send(w.ctx, alice, in) // the ack is "lost"
	retry, created, err := w.svc.Send(w.ctx, alice, in)
	if err != nil || created {
		t.Fatalf("retry: created=%v err=%v", created, err)
	}
	if retry.ID != orig.ID || retry.Seq != orig.Seq || retry.Body != orig.Body || !retry.CreatedAt.Equal(orig.CreatedAt) {
		t.Fatalf("retry returned a different message: %+v vs %+v", retry, orig)
	}
}

func TestSend_ClientIDReusedInOtherConversation(t *testing.T) {
	w := newWorld(t)
	alice := w.user("alice")
	w.user("bob")
	w.user("carol")
	c1, c2 := w.dm(alice, "bob"), w.dm(alice, "carol")
	id := uuid.New()
	if _, _, err := w.svc.Send(w.ctx, alice, chat.SendInput{ConversationID: c1, ClientID: id, Kind: "text", Body: "a"}); err != nil {
		t.Fatal(err)
	}
	_, _, err := w.svc.Send(w.ctx, alice, chat.SendInput{ConversationID: c2, ClientID: id, Kind: "text", Body: "b"})
	if code(err) != "client_id_conflict" {
		t.Fatalf("got %v", err)
	}
}

func TestSend_NonMemberDenied(t *testing.T) {
	w := newWorld(t)
	alice := w.user("alice")
	w.user("bob")
	carol := w.user("carol")
	conv := w.dm(alice, "bob")
	_, _, err := w.svc.Send(w.ctx, carol, chat.SendInput{ConversationID: conv, ClientID: uuid.New(), Kind: "text", Body: "hi"})
	if code(err) != "not_found" {
		t.Fatalf("non-member send: %v", err)
	}
	_, _, err = w.svc.Send(w.ctx, carol, chat.SendInput{ConversationID: uuid.New(), ClientID: uuid.New(), Kind: "text", Body: "hi"})
	if code(err) != "not_found" {
		t.Fatalf("unknown conversation: %v", err)
	}
	if _, err := w.svc.History(w.ctx, carol, conv, 0, 0, 50); code(err) != "not_found" {
		t.Fatalf("non-member history: %v", err)
	}
}

func TestSend_BlockedDMDenied(t *testing.T) {
	w := newWorld(t)
	alice := w.user("alice")
	bob := w.user("bob")
	conv := w.dm(alice, "bob")
	w.block(bob, alice)
	for _, from := range []uuid.UUID{alice, bob} {
		_, _, err := w.svc.Send(w.ctx, from, chat.SendInput{ConversationID: conv, ClientID: uuid.New(), Kind: "text", Body: "hi"})
		if code(err) != "blocked" {
			t.Fatalf("send from %s while blocked: %v", from, err)
		}
	}
	if _, err := w.svc.History(w.ctx, alice, conv, 0, 0, 50); err != nil {
		t.Fatalf("history stays readable after a block: %v", err)
	}
}

func TestOpenDirect_Idempotent(t *testing.T) {
	w := newWorld(t)
	alice := w.user("alice")
	bob := w.user("bob")
	a := w.dm(alice, "bob")
	b := w.dm(bob, "ALICE")
	if a != b {
		t.Fatal("A→B and B→A must open the same conversation")
	}
	var wg sync.WaitGroup
	ids := make(chan uuid.UUID, 8)
	w.user("carol")
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, err := w.svc.OpenDirect(w.ctx, alice, "carol")
			if err != nil {
				t.Error(err)
			}
			ids <- c.ID
		}()
	}
	wg.Wait()
	close(ids)
	first := <-ids
	for id := range ids {
		if id != first {
			t.Fatal("concurrent opens created two conversations")
		}
	}
	if _, err := w.svc.OpenDirect(w.ctx, alice, "alice"); code(err) != "invalid_request" {
		t.Fatalf("self DM: %v", err)
	}
}

func TestOpenDirect_BlockedLooksMissing(t *testing.T) {
	w := newWorld(t)
	alice := w.user("alice")
	bob := w.user("bob")
	w.block(bob, alice)
	_, errBlocked := w.svc.OpenDirect(w.ctx, alice, "bob")
	_, errMissing := w.svc.OpenDirect(w.ctx, alice, "nobody")
	if code(errBlocked) != "not_found" || code(errMissing) != "not_found" {
		t.Fatalf("blocked=%v missing=%v", errBlocked, errMissing)
	}
	if _, err := w.svc.OpenDirect(w.ctx, bob, "alice"); code(err) != "not_found" {
		t.Fatalf("blocker opening DM with the blocked: %v", err)
	}
}

func TestSend_Validation(t *testing.T) {
	w := newWorld(t)
	alice := w.user("alice")
	w.user("bob")
	conv := w.dm(alice, "bob")
	cases := []chat.SendInput{
		{ConversationID: conv, ClientID: uuid.New(), Kind: "text", Body: ""},
		{ConversationID: conv, ClientID: uuid.New(), Kind: "text", Body: "  \n\t "},
		{ConversationID: conv, ClientID: uuid.New(), Kind: "text", Body: strings.Repeat("é", 8001)},
		{ConversationID: conv, ClientID: uuid.New(), Kind: "system", Body: "hi"},
		{ConversationID: conv, ClientID: uuid.Nil, Kind: "text", Body: "hi"},
		{ConversationID: conv, ClientID: uuid.New(), Kind: "text", Body: "bad \x00 byte"},
	}
	for i, in := range cases {
		if _, _, err := w.svc.Send(w.ctx, alice, in); code(err) != "invalid_message" {
			t.Errorf("case %d: %v", i, err)
		}
	}
	if _, _, err := w.svc.Send(w.ctx, alice, chat.SendInput{ConversationID: conv, ClientID: uuid.New(), Kind: "text", Body: strings.Repeat("é", 8000)}); err != nil {
		t.Fatalf("8000 chars must be accepted: %v", err)
	}
}

func TestMarkRead_Monotonic(t *testing.T) {
	w := newWorld(t)
	alice := w.user("alice")
	bob := w.user("bob")
	conv := w.dm(alice, "bob")
	for i := 0; i < 6; i++ {
		w.send(alice, conv, "m")
	}
	read := func(seq int64) int64 {
		got, err := w.svc.MarkRead(w.ctx, bob, conv, seq)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	if read(5) != 5 || read(3) != 5 || read(999) != 6 {
		t.Fatal("read watermark must be monotonic and clamped to last_seq")
	}
	var delivered int64
	w.pool.QueryRow(w.ctx, `SELECT last_delivered_seq FROM members WHERE conversation_id = $1 AND user_id = $2`, conv, bob).Scan(&delivered)
	if delivered != 6 {
		t.Fatalf("reading implies delivery: delivered=%d", delivered)
	}
}

func TestList_UnreadCounts(t *testing.T) {
	w := newWorld(t)
	alice := w.user("alice")
	bob := w.user("bob")
	w.user("carol")
	c1 := w.dm(alice, "bob")
	c2 := w.dm(alice, "carol")
	w.send(bob, c1, "1")
	w.send(bob, c1, "2")
	w.send(alice, c1, "3")
	last := w.send(bob, c1, "4")
	w.send(alice, c2, "x")

	list, err := w.svc.List(w.ctx, alice)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].ID != c2 || list[1].ID != c1 {
		t.Fatalf("order by activity: %+v", list)
	}
	if list[1].Unread != 1 || list[0].Unread != 0 {
		t.Fatalf("unread: c1=%d c2=%d (sending marks your own conversation read)", list[1].Unread, list[0].Unread)
	}
	if list[1].LastMessage == nil || list[1].LastMessage.ID != last.ID {
		t.Fatal("last message")
	}
	if len(list[1].Members) != 2 {
		t.Fatalf("members: %+v", list[1].Members)
	}
}

func TestHistory_Paging(t *testing.T) {
	w := newWorld(t)
	alice := w.user("alice")
	w.user("bob")
	conv := w.dm(alice, "bob")
	for i := 0; i < 10; i++ {
		w.send(alice, conv, "m")
	}
	seqs := func(ms []chat.Message) []int64 {
		out := []int64{}
		for _, m := range ms {
			out = append(out, m.Seq)
		}
		return out
	}
	eq := func(a, b []int64) bool { return fmt.Sprint(a) == fmt.Sprint(b) }
	latest, _ := w.svc.History(w.ctx, alice, conv, 0, 0, 3)
	before, _ := w.svc.History(w.ctx, alice, conv, 5, 0, 3)
	after, _ := w.svc.History(w.ctx, alice, conv, 0, 7, 50)
	if !eq(seqs(latest), []int64{8, 9, 10}) || !eq(seqs(before), []int64{2, 3, 4}) || !eq(seqs(after), []int64{8, 9, 10}) {
		t.Fatalf("latest=%v before5=%v after7=%v", seqs(latest), seqs(before), seqs(after))
	}
	if _, err := w.svc.History(w.ctx, alice, conv, 0, 0, 101); code(err) != "invalid_request" {
		t.Fatalf("limit > 100: %v", err)
	}
}

func TestSend_NoAckBeforeCommit(t *testing.T) {
	w := newWorld(t)
	alice := w.user("alice")
	w.user("bob")
	conv := w.dm(alice, "bob")
	callsBefore := w.rec.calls
	chat.FailBeforeCommitForTesting(w.svc, errors.New("disk full"))
	_, _, err := w.svc.Send(w.ctx, alice, chat.SendInput{ConversationID: conv, ClientID: uuid.New(), Kind: "text", Body: "lost?"})
	if err == nil {
		t.Fatal("expected the injected failure")
	}
	if w.rec.calls != callsBefore {
		t.Fatal("notifier was called for an uncommitted message")
	}
	var n int
	w.pool.QueryRow(w.ctx, `SELECT count(*) FROM messages WHERE conversation_id = $1`, conv).Scan(&n)
	var last int64
	w.pool.QueryRow(w.ctx, `SELECT last_seq FROM conversations WHERE id = $1`, conv).Scan(&last)
	if n != 0 || last != 0 {
		t.Fatalf("rolled-back send left state behind: messages=%d last_seq=%d", n, last)
	}
	chat.FailBeforeCommitForTesting(w.svc, nil)
	if m := w.send(alice, conv, "after"); m.Seq != 1 {
		t.Fatalf("seq after a rollback = %d, want 1 (gap-free)", m.Seq)
	}
}

func TestEvents_Replay(t *testing.T) {
	w := newWorld(t)
	alice := w.user("alice")
	bob := w.user("bob")
	conv := w.dm(alice, "bob")
	w.send(alice, conv, "1")
	w.send(alice, conv, "2")
	evs, err := w.svc.EventsAfter(w.ctx, bob, 1, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 2 || evs[0].Seq != 2 || evs[1].Seq != 3 || evs[0].Type != "message.created" {
		t.Fatalf("events after 1: %+v", evs)
	}
	if oldest, _ := w.svc.OldestCursor(w.ctx, bob); oldest != 0 {
		t.Fatalf("oldest cursor = %d", oldest)
	}
	w.pool.Exec(w.ctx, `DELETE FROM user_events WHERE user_id = $1 AND seq <= 2`, bob)
	if oldest, _ := w.svc.OldestCursor(w.ctx, bob); oldest != 2 {
		t.Fatalf("after pruning, oldest cursor = %d, want 2", oldest)
	}
}
