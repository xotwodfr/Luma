package realtime

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"

	"luma.folk3s.com/server/internal/auth"
	"luma.folk3s.com/server/internal/chat"
	"luma.folk3s.com/server/internal/httpx"
)

const (
	closeUnauthorized websocket.StatusCode = 4001
	closeBadHello     websocket.StatusCode = 4002
	closeRevoked      websocket.StatusCode = 4003
	closeTooSlow      websocket.StatusCode = 4008
	closeGoingAway                         = websocket.StatusGoingAway

	outBuffer     = 1024
	replayPage    = 500
	pingInterval  = 25 * time.Second
	pingTimeout   = 35 * time.Second
	writeTimeout  = 10 * time.Second
	maxFrameBytes = 128 << 10
)

// frame is the wire envelope: {"t": type, "id": request id, "d": payload}.
type frame struct {
	T  string `json:"t"`
	ID string `json:"id,omitempty"`
	D  any    `json:"d,omitempty"`
}

type inFrame struct {
	T  string          `json:"t"`
	ID string          `json:"id"`
	D  json.RawMessage `json:"d"`
}

type outItem struct {
	f  *frame
	ev *chat.Event
}

type conn struct {
	hub       *Hub
	ws        *websocket.Conn
	p         auth.Principal
	out       chan outItem
	ctx       context.Context
	cancel    context.CancelFunc
	closeOnce sync.Once
	lastSent  int64 // writer goroutine only
}

func (c *conn) push(it outItem) {
	select {
	case c.out <- it:
	default:
		c.close(closeTooSlow, "client too slow; reconnect to replay")
	}
}

// close sends the close frame first and only then cancels: cancelling a pending Read in
// coder/websocket tears the connection down at once, and the client would never see the code
// (4003 tells it to sign out instead of reconnecting).
func (c *conn) close(code websocket.StatusCode, reason string) {
	c.closeOnce.Do(func() {
		go func() {
			c.ws.Close(code, reason)
			c.cancel()
		}()
	})
}

// ServeHTTP upgrades GET /v1/realtime. Auth comes from the Authorization header, or from the
// first frame (hello.token) for platforms that cannot set headers on a WebSocket handshake.
func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var p auth.Principal
	preAuthed := false
	if tok := auth.BearerToken(r); tok != "" {
		var err error
		if p, err = h.o.Auth.Authenticate(r.Context(), tok); err != nil {
			httpx.Fail(w, r, err)
			return
		}
		preAuthed = true
	}
	ws, err := websocket.Accept(w, r, nil)
	if err != nil {
		return // Accept already wrote the HTTP error (e.g. 426 for a non-upgrade request)
	}
	ws.SetReadLimit(maxFrameBytes)

	helloCtx, cancelHello := context.WithTimeout(r.Context(), h.o.HelloTimeout)
	_, data, err := ws.Read(helloCtx)
	cancelHello()
	if err != nil {
		ws.Close(closeUnauthorized, "hello timeout")
		return
	}
	var hello struct {
		Token  string `json:"token"`
		Cursor *int64 `json:"cursor"` // null: fresh client, no replay
	}
	var in inFrame
	if json.Unmarshal(data, &in) != nil || in.T != "hello" || json.Unmarshal(in.D, &hello) != nil {
		ws.Close(closeBadHello, "first frame must be hello")
		return
	}
	if !preAuthed {
		if p, err = h.o.Auth.Authenticate(r.Context(), hello.Token); err != nil {
			ws.Close(closeUnauthorized, "unauthorized")
			return
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	c := &conn{hub: h, ws: ws, p: p, out: make(chan outItem, outBuffer), ctx: ctx, cancel: cancel}
	first := h.register(c)
	// Close the window where a revoke lands between Authenticate and register.
	var live bool
	if err := h.o.Pool.QueryRow(ctx, `SELECT revoked_at IS NULL FROM sessions WHERE id = $1`, p.SessionID).Scan(&live); err != nil || !live {
		h.unregister(c)
		c.close(closeRevoked, "session revoked")
		return
	}
	if first {
		h.broadcastPresence(ctx, p.UserID, true, nil)
	}
	go c.writer(hello.Cursor)
	go c.pinger()
	c.reader()

	c.close(websocket.StatusNormalClosure, "")
	if h.unregister(c) {
		now := time.Now().UTC()
		bg, done := context.WithTimeout(context.Background(), 5*time.Second)
		_, _ = h.o.Pool.Exec(bg, `UPDATE users SET last_seen_at = $2 WHERE id = $1`, p.UserID, now)
		if !h.online(p.UserID) { // a fast reconnect may already have announced "online"
			h.broadcastPresence(bg, p.UserID, false, &now)
		}
		done()
	}
}

func (c *conn) write(f frame) error {
	b, err := json.Marshal(f)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(c.ctx, writeTimeout)
	defer cancel()
	return c.ws.Write(ctx, websocket.MessageText, b)
}

func (c *conn) writeEvent(e chat.Event) error {
	if err := c.write(frame{T: "event", D: e}); err != nil {
		return err
	}
	c.lastSent = e.Seq
	return nil
}

// catchUp sends every stored event after lastSent, up to and including through (0 = all).
func (c *conn) catchUp(through int64) error {
	for {
		evs, err := c.hub.o.Chat.EventsAfter(c.ctx, c.p.UserID, c.lastSent, replayPage)
		if err != nil {
			return err
		}
		for _, e := range evs {
			if err := c.writeEvent(e); err != nil {
				return err
			}
			if through > 0 && e.Seq >= through {
				return nil
			}
		}
		if len(evs) < replayPage {
			return nil
		}
	}
}

// writer is the only goroutine that writes to the socket. It replays from cursor, sends ready,
// then delivers live items in seq order. Per user, event seqs commit in order (the user row lock
// is held until COMMIT), but post-commit callbacks can arrive out of order — so a gap triggers a
// catch-up from the database instead of dropping or reordering.
func (c *conn) writer(cursor *int64) {
	defer c.close(websocket.StatusInternalError, "write failed")
	ctx := c.ctx
	chatSvc := c.hub.o.Chat
	oldest, err1 := chatSvc.OldestCursor(ctx, c.p.UserID)
	current, err2 := chatSvc.CurrentCursor(ctx, c.p.UserID)
	if err := errors.Join(err1, err2); err != nil {
		return
	}
	if cursor == nil || *cursor < oldest || *cursor > current {
		c.lastSent = current
		if c.write(frame{T: "resync_required", D: map[string]int64{"cursor": current}}) != nil {
			return
		}
	} else {
		c.lastSent = *cursor
		if c.catchUp(0) != nil {
			return
		}
	}
	me, err := c.hub.me(ctx, c.p.UserID)
	if err != nil || c.write(frame{T: "ready", D: map[string]any{"cursor": c.lastSent, "user": me}}) != nil {
		return
	}
	if ps, err := c.hub.partners(ctx, c.p.UserID); err == nil {
		for _, p := range ps {
			p.Online = c.hub.online(p.UserID)
			if c.write(frame{T: "presence", D: p}) != nil {
				return
			}
		}
	}
	for {
		select {
		case <-ctx.Done():
			return
		case it := <-c.out:
			var err error
			switch {
			case it.ev == nil:
				err = c.write(*it.f)
			case it.ev.Seq <= c.lastSent:
				continue // already delivered by replay or catch-up
			case it.ev.Seq == c.lastSent+1:
				err = c.writeEvent(*it.ev)
			default:
				err = c.catchUp(it.ev.Seq)
			}
			if err != nil {
				return
			}
		}
	}
}

func (c *conn) pinger() {
	t := time.NewTicker(pingInterval)
	defer t.Stop()
	for {
		select {
		case <-c.ctx.Done():
			return
		case <-t.C:
			ctx, cancel := context.WithTimeout(c.ctx, pingTimeout)
			err := c.ws.Ping(ctx)
			cancel()
			if err != nil {
				c.close(closeGoingAway, "ping timeout")
				return
			}
		}
	}
}

func (c *conn) reply(id string, d any) { c.push(outItem{f: &frame{T: "ack", ID: id, D: d}}) }
func (c *conn) fail(id string, code, message string) {
	c.push(outItem{f: &frame{T: "error", ID: id, D: map[string]string{"code": code, "message": message}}})
}

func (c *conn) failErr(id string, err error) {
	var e *httpx.Error
	if errors.As(err, &e) {
		c.fail(id, e.Code, e.Message)
		return
	}
	if c.ctx.Err() == nil {
		c.hub.o.Logger.Error("realtime internal error", "err", err, "user", c.p.UserID)
	}
	c.fail(id, httpx.ErrInternal.Code, httpx.ErrInternal.Message)
}

var errBadFrame = httpx.E(http.StatusBadRequest, "invalid_request", "Malformed frame.")

func (c *conn) reader() {
	for {
		_, data, err := c.ws.Read(c.ctx)
		if err != nil {
			return
		}
		var in inFrame
		if json.Unmarshal(data, &in) != nil {
			c.failErr("", errBadFrame)
			continue
		}
		c.handle(in)
	}
}

func (c *conn) handle(in inFrame) {
	var target struct {
		ConversationID uuid.UUID `json:"conversation_id"`
		Seq            int64     `json:"seq"`
	}
	switch in.T {
	case "send":
		if !c.allowSend(in.ID) {
			return
		}
		var s chat.SendInput
		if json.Unmarshal(in.D, &s) != nil {
			c.failErr(in.ID, errBadFrame)
			return
		}
		m, _, err := c.hub.o.Chat.Send(c.ctx, c.p.UserID, s)
		if err != nil {
			c.failErr(in.ID, err)
			return
		}
		c.reply(in.ID, map[string]any{"message": m})
	case "read", "delivered":
		if json.Unmarshal(in.D, &target) != nil {
			c.failErr(in.ID, errBadFrame)
			return
		}
		mark, key := c.hub.o.Chat.MarkRead, "last_read_seq"
		if in.T == "delivered" {
			mark, key = c.hub.o.Chat.MarkDelivered, "last_delivered_seq"
		}
		seq, err := mark(c.ctx, c.p.UserID, target.ConversationID, target.Seq)
		if err != nil {
			c.failErr(in.ID, err)
			return
		}
		c.reply(in.ID, map[string]int64{key: seq})
	case "typing":
		if json.Unmarshal(in.D, &target) != nil {
			c.failErr(in.ID, errBadFrame)
			return
		}
		if err := c.hub.sendTyping(c.ctx, c.p.UserID, target.ConversationID); err != nil {
			c.failErr(in.ID, err)
		}
	case "ping":
		c.push(outItem{f: &frame{T: "pong", ID: in.ID}})
	default:
		c.fail(in.ID, "unknown_frame", "Unknown frame type.")
	}
}

func (c *conn) allowSend(id string) bool {
	if c.hub.o.SendLimit == nil {
		return true
	}
	if ok, _ := c.hub.o.SendLimit.Allow(c.p.UserID.String()); !ok {
		c.fail(id, "rate_limited", "Too many messages. Try again shortly.")
		return false
	}
	return true
}
