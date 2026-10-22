package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"

	"luma.folk3s.com/server/internal/api"
	"luma.folk3s.com/server/internal/chat"
	"luma.folk3s.com/server/internal/config"
)

type frame struct {
	T  string          `json:"t"`
	ID string          `json:"id,omitempty"`
	D  json.RawMessage `json:"d,omitempty"`
}

type event struct {
	Seq  int64           `json:"seq"`
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

func (f frame) event() event {
	var e event
	_ = json.Unmarshal(f.D, &e)
	return e
}

func (f frame) messageBody() string {
	var d struct {
		Message chat.Message `json:"message"`
	}
	e := f.event()
	_ = json.Unmarshal(e.Data, &d)
	return d.Message.Body
}

type wsClient struct {
	t      *testing.T
	c      *websocket.Conn
	frames chan frame
	closed chan error
	cursor int64
	// replayed holds the event frames received before "ready".
	replayed []frame
}

type dialOpts struct {
	fresh      bool // hello with cursor null
	headerAuth bool
	noHello    bool
}

func (h *harness) wsURL() string {
	return "ws" + strings.TrimPrefix(h.srv.URL, "http") + "/v1/realtime"
}

func dialWS(t *testing.T, url, token string, cursor int64, o dialOpts) *wsClient {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	hdr := http.Header{}
	if o.headerAuth {
		hdr.Set("Authorization", "Bearer "+token)
	}
	c, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: hdr})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	c.SetReadLimit(1 << 20)
	w := &wsClient{t: t, c: c, frames: make(chan frame, 4096), closed: make(chan error, 1)}
	go func() {
		for {
			_, b, err := c.Read(context.Background())
			if err != nil {
				w.closed <- err
				close(w.frames)
				return
			}
			var f frame
			_ = json.Unmarshal(b, &f)
			w.frames <- f
		}
	}()
	t.Cleanup(func() { c.Close(websocket.StatusNormalClosure, "") })
	if o.noHello {
		return w
	}
	hello := map[string]any{"cursor": cursor}
	if o.fresh {
		hello["cursor"] = nil
	}
	if !o.headerAuth {
		hello["token"] = token
	}
	w.write("hello", "", hello)
	for {
		f := w.next(5 * time.Second)
		switch f.T {
		case "event":
			w.replayed = append(w.replayed, f)
		case "resync_required":
			w.replayed = append(w.replayed, f)
		case "ready":
			var d struct{ Cursor int64 }
			_ = json.Unmarshal(f.D, &d)
			w.cursor = d.Cursor
			return w
		case "error":
			t.Fatalf("hello failed: %s", f.D)
		}
	}
}

func (w *wsClient) write(t, id string, d any) {
	w.t.Helper()
	b, _ := json.Marshal(map[string]any{"t": t, "id": id, "d": d})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := w.c.Write(ctx, websocket.MessageText, b); err != nil {
		w.t.Fatalf("write %s: %v", t, err)
	}
}

func (w *wsClient) next(timeout time.Duration) frame {
	w.t.Helper()
	select {
	case f, ok := <-w.frames:
		if !ok {
			w.t.Fatal("socket closed while waiting for a frame")
		}
		return f
	case <-time.After(timeout):
		w.t.Fatal("timed out waiting for a frame")
	}
	return frame{}
}

// expect waits for the first frame matching pred, skipping others (presence, pongs…).
func (w *wsClient) expect(pred func(frame) bool, timeout time.Duration) frame {
	w.t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case f, ok := <-w.frames:
			if !ok {
				w.t.Fatal("socket closed while waiting")
			}
			if pred(f) {
				return f
			}
		case <-deadline:
			w.t.Fatal("timed out waiting for matching frame")
		}
	}
}

// quiet asserts that no frame matching pred arrives within d.
func (w *wsClient) quiet(pred func(frame) bool, d time.Duration) {
	w.t.Helper()
	deadline := time.After(d)
	for {
		select {
		case f, ok := <-w.frames:
			if !ok {
				return
			}
			if pred(f) {
				w.t.Fatalf("unexpected frame %s %s", f.T, f.D)
			}
		case <-deadline:
			return
		}
	}
}

func isEvent(typ string) func(frame) bool {
	return func(f frame) bool { return f.T == "event" && f.event().Type == typ }
}

func (w *wsClient) send(conv, body string) string {
	id := uuid.NewString()
	w.write("send", id, map[string]any{"conversation_id": conv, "client_id": uuid.NewString(), "kind": "text", "body": body})
	return id
}

func (h *harness) openDM(a user, b string) string {
	h.t.Helper()
	r := h.do("POST", "/v1/conversations", a.Token, map[string]any{"kind": "direct", "username": b})
	if r.Status != 200 {
		h.t.Fatalf("open DM: %d %s", r.Status, r.Raw)
	}
	return r.Body["id"].(string)
}

func (h *harness) restSend(u user, conv, body string) {
	h.t.Helper()
	r := h.do("POST", "/v1/conversations/"+conv+"/messages", u.Token, map[string]any{"client_id": uuid.NewString(), "kind": "text", "body": body})
	if r.Status != 201 {
		h.t.Fatalf("send: %d %s", r.Status, r.Raw)
	}
}

func TestRealtime_SendAckAfterCommit(t *testing.T) {
	h := newHarness(t)
	a, b := h.register("alice"), h.register("bob")
	conv := h.openDM(a, "bob")
	wa := dialWS(t, h.wsURL(), a.Token, 0, dialOpts{headerAuth: true})
	wb := dialWS(t, h.wsURL(), b.Token, 0, dialOpts{})
	id := wa.send(conv, "hello bob")
	ack := wa.expect(func(f frame) bool { return f.T == "ack" && f.ID == id }, 3*time.Second)
	var d struct{ Message chat.Message }
	_ = json.Unmarshal(ack.D, &d)
	if d.Message.Seq != 1 || d.Message.Body != "hello bob" {
		t.Fatalf("ack: %s", ack.D)
	}
	var n int
	h.pool.QueryRow(context.Background(), `SELECT count(*) FROM messages WHERE id = $1`, d.Message.ID).Scan(&n)
	if n != 1 {
		t.Fatal("acked message is not in the database")
	}
	got := wb.expect(isEvent("message.created"), 3*time.Second)
	if got.messageBody() != "hello bob" {
		t.Fatalf("bob got %s", got.D)
	}
}

func TestRealtime_SecondDeviceReceivesOwnMessage(t *testing.T) {
	h := newHarness(t)
	a := h.register("alice")
	h.register("bob")
	conv := h.openDM(a, "bob")
	phone := dialWS(t, h.wsURL(), a.Token, 0, dialOpts{})
	laptopToken := h.login("alice", "password-alice").Body["token"].(string)
	laptop := dialWS(t, h.wsURL(), laptopToken, 0, dialOpts{})
	phone.send(conv, "from my phone")
	if f := laptop.expect(isEvent("message.created"), 3*time.Second); f.messageBody() != "from my phone" {
		t.Fatalf("laptop got %s", f.D)
	}
}

func TestRealtime_ReplayMissedEvents(t *testing.T) {
	h := newHarness(t)
	a, b := h.register("alice"), h.register("bob")
	conv := h.openDM(a, "bob")
	wb := dialWS(t, h.wsURL(), b.Token, 0, dialOpts{})
	cursor := wb.cursor
	wb.c.Close(websocket.StatusNormalClosure, "")
	for i := 1; i <= 5; i++ {
		h.restSend(a, conv, fmt.Sprintf("m%d", i))
	}
	wb2 := dialWS(t, h.wsURL(), b.Token, cursor, dialOpts{})
	if len(wb2.replayed) != 5 {
		t.Fatalf("replayed %d events, want 5", len(wb2.replayed))
	}
	for i, f := range wb2.replayed {
		if f.messageBody() != fmt.Sprintf("m%d", i+1) || f.event().Seq != cursor+int64(i)+1 {
			t.Fatalf("replay[%d] = seq %d %q", i, f.event().Seq, f.messageBody())
		}
	}
	if wb2.cursor != cursor+5 {
		t.Fatalf("ready cursor %d", wb2.cursor)
	}
	h.restSend(a, conv, "live")
	if f := wb2.expect(isEvent("message.created"), 3*time.Second); f.messageBody() != "live" {
		t.Fatalf("live: %s", f.D)
	}
}

func TestRealtime_ReplayLiveRace(t *testing.T) {
	h := newHarness(t)
	a, b := h.register("alice"), h.register("bob")
	conv := h.openDM(a, "bob")
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 40; i++ {
			h.restSend(a, conv, "x")
		}
	}()
	time.Sleep(20 * time.Millisecond)
	wb := dialWS(t, h.wsURL(), b.Token, 0, dialOpts{})
	wg.Wait()
	seen := map[int64]bool{}
	last := int64(0)
	record := func(f frame) {
		s := f.event().Seq
		if seen[s] {
			t.Fatalf("event seq %d delivered twice", s)
		}
		if s <= last {
			t.Fatalf("event seq %d after %d: out of order", s, last)
		}
		seen[s], last = true, s
	}
	for _, f := range wb.replayed {
		record(f)
	}
	for last < 41 { // conversation.created + 40 messages
		record(wb.expect(func(f frame) bool { return f.T == "event" }, 5*time.Second))
	}
	wb.quiet(func(f frame) bool { return f.T == "event" }, 300*time.Millisecond)
}

func TestRealtime_RestartDurability(t *testing.T) {
	h := newHarness(t)
	a, b := h.register("alice"), h.register("bob")
	conv := h.openDM(a, "bob")
	wa := dialWS(t, h.wsURL(), a.Token, 0, dialOpts{})
	acked := 0
	for i := 0; i < 20; i++ {
		id := wa.send(conv, fmt.Sprintf("m%d", i))
		wa.expect(func(f frame) bool { return f.T == "ack" && f.ID == id }, 3*time.Second)
		acked++
	}
	// Kill the first server abruptly.
	h.srv.CloseClientConnections()
	h.srv.Close()
	h.app.Close()

	app2 := api.New(api.Deps{Pool: h.pool, Config: config.Config{Registration: "invite", UploadDir: t.TempDir()}, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), AuthPerMinute: 1000})
	srv2 := httptest.NewServer(app2.Handler())
	defer srv2.Close()
	defer app2.Close()
	wb := dialWS(t, "ws"+strings.TrimPrefix(srv2.URL, "http")+"/v1/realtime", b.Token, 0, dialOpts{})
	msgs := 0
	for _, f := range wb.replayed {
		if f.event().Type == "message.created" {
			msgs++
		}
	}
	var stored int
	h.pool.QueryRow(context.Background(), `SELECT count(*) FROM messages`).Scan(&stored)
	if msgs != acked || stored != acked {
		t.Fatalf("acked=%d replayed=%d stored=%d", acked, msgs, stored)
	}
}

func TestRealtime_FailedCommitNotAcked(t *testing.T) {
	h := newHarness(t)
	a := h.register("alice")
	h.register("bob")
	conv := h.openDM(a, "bob")
	wa := dialWS(t, h.wsURL(), a.Token, 0, dialOpts{})
	chat.FailBeforeCommitForTesting(h.app.Chat, errors.New("injected"))
	id := wa.send(conv, "doomed")
	f := wa.expect(func(f frame) bool { return f.ID == id }, 3*time.Second)
	if f.T != "error" {
		t.Fatalf("got %s %s; an uncommitted send must not be acked", f.T, f.D)
	}
	chat.FailBeforeCommitForTesting(h.app.Chat, nil)
}

func TestRealtime_DuplicateSendAcksSameMessage(t *testing.T) {
	h := newHarness(t)
	a := h.register("alice")
	h.register("bob")
	conv := h.openDM(a, "bob")
	wa := dialWS(t, h.wsURL(), a.Token, 0, dialOpts{})
	clientID := uuid.NewString()
	var seqs []int64
	for _, id := range []string{"r1", "r2"} {
		wa.write("send", id, map[string]any{"conversation_id": conv, "client_id": clientID, "kind": "text", "body": "once"})
		ack := wa.expect(func(f frame) bool { return f.T == "ack" && f.ID == id }, 3*time.Second)
		var d struct{ Message chat.Message }
		_ = json.Unmarshal(ack.D, &d)
		seqs = append(seqs, d.Message.Seq)
	}
	if seqs[0] != seqs[1] {
		t.Fatalf("duplicate send acked with different seqs %v", seqs)
	}
}

func TestRealtime_SendBlockedDenied(t *testing.T) {
	h := newHarness(t)
	a, m := h.register("alice"), h.register("mallory")
	conv := h.openDM(a, "mallory")
	h.do("POST", "/v1/users/mallory/block", a.Token, nil)
	wm := dialWS(t, h.wsURL(), m.Token, 0, dialOpts{})
	id := wm.send(conv, "hey")
	f := wm.expect(func(f frame) bool { return f.ID == id }, 3*time.Second)
	if f.T != "error" || !strings.Contains(string(f.D), `"blocked"`) {
		t.Fatalf("got %s %s", f.T, f.D)
	}
}

func TestRealtime_RevokeClosesSocket(t *testing.T) {
	h := newHarness(t)
	a := h.register("alice")
	ws := dialWS(t, h.wsURL(), a.Token, 0, dialOpts{})
	other := h.login("alice", "password-alice").Body["token"].(string)
	start := time.Now()
	if r := h.do("POST", "/v1/auth/sessions/revoke-others", other, nil); r.Status != 204 {
		t.Fatalf("revoke: %d", r.Status)
	}
	select {
	case err := <-ws.closed:
		if time.Since(start) > time.Second {
			t.Fatalf("socket closed after %v", time.Since(start))
		}
		// Clients rely on 4003 to sign out instead of reconnecting (protocol/realtime.md).
		if code := websocket.CloseStatus(err); code != 4003 {
			t.Fatalf("revoked socket closed with %v, want 4003", code)
		}
	case <-time.After(time.Second):
		t.Fatal("revoked session's socket still open after 1s")
	}
}

func TestRealtime_HelloTimeout(t *testing.T) {
	h := newHarness(t, func(d *api.Deps) { d.HelloTimeout = 200 * time.Millisecond })
	ws := dialWS(t, h.wsURL(), "", 0, dialOpts{noHello: true})
	select {
	case <-ws.closed:
	case <-time.After(2 * time.Second):
		t.Fatal("connection without hello was not closed")
	}
}

func TestRealtime_BadTokenRejected(t *testing.T) {
	h := newHarness(t)
	ws := dialWS(t, h.wsURL(), "", 0, dialOpts{noHello: true})
	ws.write("hello", "", map[string]any{"token": "nope", "cursor": 0})
	select {
	case err := <-ws.closed:
		if websocket.CloseStatus(err) != 4001 {
			t.Fatalf("close status %v", websocket.CloseStatus(err))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("bad token not rejected")
	}
}

func TestRealtime_TypingNotSentToBlocked(t *testing.T) {
	h := newHarness(t)
	a, b, m := h.register("alice"), h.register("bob"), h.register("mallory")
	ab := h.openDM(a, "bob")
	am := h.openDM(a, "mallory")
	h.do("POST", "/v1/users/mallory/block", a.Token, nil)
	wa := dialWS(t, h.wsURL(), a.Token, 0, dialOpts{})
	wb := dialWS(t, h.wsURL(), b.Token, 0, dialOpts{})
	wm := dialWS(t, h.wsURL(), m.Token, 0, dialOpts{})
	wm.write("typing", "", map[string]any{"conversation_id": am})
	wa.write("typing", "", map[string]any{"conversation_id": ab})
	wb.expect(func(f frame) bool { return f.T == "typing" }, 2*time.Second)
	wa.quiet(func(f frame) bool { return f.T == "typing" }, 300*time.Millisecond)
	wm.quiet(func(f frame) bool { return f.T == "typing" }, 100*time.Millisecond)
}

func TestRealtime_PresenceOnlyToPartners(t *testing.T) {
	h := newHarness(t)
	a, b, c := h.register("alice"), h.register("bob"), h.register("carol")
	h.openDM(a, "bob")
	wb := dialWS(t, h.wsURL(), b.Token, 0, dialOpts{})
	wc := dialWS(t, h.wsURL(), c.Token, 0, dialOpts{})
	wa := dialWS(t, h.wsURL(), a.Token, 0, dialOpts{})
	isAlice := func(online bool) func(frame) bool {
		return func(f frame) bool {
			return f.T == "presence" && strings.Contains(string(f.D), a.ID) && strings.Contains(string(f.D), fmt.Sprintf(`"online":%v`, online))
		}
	}
	wb.expect(isAlice(true), 2*time.Second)
	wc.quiet(func(f frame) bool { return f.T == "presence" && strings.Contains(string(f.D), a.ID) }, 300*time.Millisecond)
	wa.c.Close(websocket.StatusNormalClosure, "")
	wb.expect(isAlice(false), 2*time.Second)
}

func TestRealtime_StaleCursorResync(t *testing.T) {
	h := newHarness(t)
	a, b := h.register("alice"), h.register("bob")
	conv := h.openDM(a, "bob")
	h.restSend(a, conv, "1")
	h.restSend(a, conv, "2")
	h.pool.Exec(context.Background(), `DELETE FROM user_events WHERE user_id = $1 AND seq <= 2`, b.ID)
	wb := dialWS(t, h.wsURL(), b.Token, 0, dialOpts{})
	if len(wb.replayed) != 1 || wb.replayed[0].T != "resync_required" {
		t.Fatalf("want resync_required, got %+v", wb.replayed)
	}
	if wb.cursor != 3 {
		t.Fatalf("after resync the cursor is the current one: %d", wb.cursor)
	}
}

// Review finding #2: the admin CLI is a separate process; its revocations must still close
// sockets held by the running server (via Postgres NOTIFY).
func TestRealtime_RevokeFromOtherProcessClosesSocket(t *testing.T) {
	h := newHarness(t)
	a := h.register("alice")
	ws := dialWS(t, h.wsURL(), a.Token, 0, dialOpts{})
	cli := api.New(api.Deps{Pool: h.pool, Config: config.Config{Registration: "invite", UploadDir: t.TempDir()}, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	defer cli.Close()
	start := time.Now()
	if err := cli.Auth.DisableUser(context.Background(), "alice"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ws.closed:
		t.Logf("closed after %v", time.Since(start))
	case <-time.After(time.Second):
		t.Fatal("socket of a user disabled from another process still open after 1s")
	}
}

// Review finding #5: a fresh install must not replay the whole account history.
func TestRealtime_FreshClientGetsResyncNotReplay(t *testing.T) {
	h := newHarness(t)
	a, b := h.register("alice"), h.register("bob")
	conv := h.openDM(a, "bob")
	for i := 0; i < 3; i++ {
		h.restSend(a, conv, "history")
	}
	wb := dialWS(t, h.wsURL(), b.Token, 0, dialOpts{fresh: true})
	if len(wb.replayed) != 1 || wb.replayed[0].T != "resync_required" || wb.cursor != 4 {
		t.Fatalf("fresh client: replayed=%d cursor=%d", len(wb.replayed), wb.cursor)
	}
}

// Security review: once a session is revoked, its socket must not get any further sends
// accepted while the close handshake is still in flight.
func TestRealtime_NoSendsAcceptedAfterRevoke(t *testing.T) {
	h := newHarness(t)
	a := h.register("alice")
	h.register("bob")
	conv := h.openDM(a, "bob")
	ws := dialWS(t, h.wsURL(), a.Token, 0, dialOpts{})
	other := h.login("alice", "password-alice").Body["token"].(string)
	if r := h.do("POST", "/v1/auth/sessions/revoke-others", other, nil); r.Status != 204 {
		t.Fatalf("revoke: %d", r.Status)
	}
	var before int
	h.pool.QueryRow(context.Background(), `SELECT count(*) FROM messages`).Scan(&before)
	for i := 0; i < 20; i++ {
		b, _ := json.Marshal(map[string]any{"t": "send", "id": fmt.Sprint(i), "d": map[string]any{"conversation_id": conv, "client_id": uuid.NewString(), "kind": "text", "body": "after revoke"}})
		if ws.c.Write(context.Background(), websocket.MessageText, b) != nil {
			break
		}
	}
	time.Sleep(300 * time.Millisecond)
	var after int
	h.pool.QueryRow(context.Background(), `SELECT count(*) FROM messages`).Scan(&after)
	if after != before {
		t.Fatalf("%d messages accepted from a revoked session", after-before)
	}
}

// Live fan-out follows membership: an added member starts receiving the group's messages
// and typing; a removed one gets their removal notice and then nothing, over REST or WS.
func TestRealtime_GroupFanOutFollowsMembership(t *testing.T) {
	h := newHarness(t)
	a, _, c := h.register("alice"), h.register("bob"), h.register("carol")
	r := h.do("POST", "/v1/conversations", a.Token, map[string]any{"kind": "group", "title": "G", "usernames": []string{"bob"}})
	g := r.Body["id"].(string)
	wa := dialWS(t, h.wsURL(), a.Token, 0, dialOpts{})
	wc := dialWS(t, h.wsURL(), c.Token, 0, dialOpts{})
	h.restSend(a, g, "before carol")
	wc.quiet(isEvent("message.created"), 200*time.Millisecond)

	h.do("POST", "/v1/conversations/"+g+"/members", a.Token, map[string]any{"usernames": []string{"carol"}})
	wc.expect(isEvent("membership.updated"), 2*time.Second)
	wa.send(g, "hi carol")
	if f := wc.expect(func(f frame) bool { return isEvent("message.created")(f) && f.messageBody() != "" }, 2*time.Second); f.messageBody() != "hi carol" {
		t.Fatalf("carol got %s", f.D)
	}
	wa.write("typing", "", map[string]any{"conversation_id": g})
	wc.expect(func(f frame) bool { return f.T == "typing" }, 2*time.Second)

	if r := h.do("DELETE", "/v1/conversations/"+g+"/members/"+c.ID, a.Token, nil); r.Status != 200 {
		t.Fatalf("remove: %d %s", r.Status, r.Raw)
	}
	wc.expect(isEvent("membership.updated"), 2*time.Second)
	wc.expect(isEvent("message.created"), 2*time.Second) // the removal notice is inside her window
	h.restSend(a, g, "after carol")
	time.Sleep(1100 * time.Millisecond) // typing is throttled to 1/s per sender+conversation
	wa.write("typing", "", map[string]any{"conversation_id": g})
	wc.quiet(func(f frame) bool { return isEvent("message.created")(f) || f.T == "typing" }, 400*time.Millisecond)
	wc.write("typing", "", map[string]any{"conversation_id": g}) // former member's typing goes nowhere
	wa.quiet(func(f frame) bool { return f.T == "typing" }, 300*time.Millisecond)
	id := wc.send(g, "can I still post?")
	if f := wc.expect(func(f frame) bool { return f.ID == id }, 2*time.Second); f.T != "error" {
		t.Fatalf("removed member's WS send: %s %s", f.T, f.D)
	}
}
