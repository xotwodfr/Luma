// Package realtime is Luma's WebSocket layer: authenticated connections, gap-free
// event replay on reconnect, live fan-out after commit, typing and presence.
package realtime

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"luma.folk3s.com/server/internal/auth"
	"luma.folk3s.com/server/internal/chat"
	"luma.folk3s.com/server/internal/httpx"
	"luma.folk3s.com/server/internal/users"
)

type Options struct {
	Chat      *chat.Service
	Auth      *auth.Service
	Pool      *pgxpool.Pool
	Logger    *slog.Logger
	SendLimit *httpx.Limiter
	// HelloTimeout bounds how long an unauthenticated socket may stay open. Default 10s.
	HelloTimeout time.Duration
}

// Hub tracks live connections by user and by session.
// ponytail: single-process fan-out; use Postgres LISTEN/NOTIFY to run more than one instance.
type Hub struct {
	o         Options
	typing    *httpx.Limiter
	mu        sync.Mutex
	byUser    map[uuid.UUID]map[*conn]struct{}
	bySession map[uuid.UUID]map[*conn]struct{}
}

func NewHub(o Options) *Hub {
	if o.HelloTimeout == 0 {
		o.HelloTimeout = 10 * time.Second
	}
	return &Hub{
		o:         o,
		typing:    httpx.NewLimiter(1, time.Second),
		byUser:    map[uuid.UUID]map[*conn]struct{}{},
		bySession: map[uuid.UUID]map[*conn]struct{}{},
	}
}

func add(m map[uuid.UUID]map[*conn]struct{}, k uuid.UUID, c *conn) {
	if m[k] == nil {
		m[k] = map[*conn]struct{}{}
	}
	m[k][c] = struct{}{}
}

// register adds c and reports whether it is the user's first live connection.
func (h *Hub) register(c *conn) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	first := len(h.byUser[c.p.UserID]) == 0
	add(h.byUser, c.p.UserID, c)
	add(h.bySession, c.p.SessionID, c)
	return first
}

// unregister removes c and reports whether the user has no live connections left.
func (h *Hub) unregister(c *conn) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.byUser[c.p.UserID], c)
	delete(h.bySession[c.p.SessionID], c)
	if len(h.bySession[c.p.SessionID]) == 0 {
		delete(h.bySession, c.p.SessionID)
	}
	if len(h.byUser[c.p.UserID]) == 0 {
		delete(h.byUser, c.p.UserID)
		return true
	}
	return false
}

func (h *Hub) conns(user uuid.UUID) []*conn {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]*conn, 0, len(h.byUser[user]))
	for c := range h.byUser[user] {
		out = append(out, c)
	}
	return out
}

func (h *Hub) online(user uuid.UUID) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.byUser[user]) > 0
}

// Committed implements chat.Notifier: it never blocks; slow sockets are closed and will replay.
func (h *Hub) Committed(events []chat.Event) {
	for i := range events {
		for _, c := range h.conns(events[i].UserID) {
			c.push(outItem{ev: &events[i]})
		}
	}
}

// CloseSession closes every socket of a revoked session.
func (h *Hub) CloseSession(id uuid.UUID) {
	h.mu.Lock()
	var cs []*conn
	for c := range h.bySession[id] {
		cs = append(cs, c)
	}
	h.mu.Unlock()
	for _, c := range cs {
		c.close(closeRevoked, "session revoked")
	}
}

// Close closes all sockets (server shutdown); clients reconnect and replay.
func (h *Hub) Close() {
	h.mu.Lock()
	var cs []*conn
	for _, set := range h.byUser {
		for c := range set {
			cs = append(cs, c)
		}
	}
	h.mu.Unlock()
	for _, c := range cs {
		c.close(closeGoingAway, "server restarting")
	}
}

type presenceFrame struct {
	UserID   uuid.UUID  `json:"user_id"`
	Online   bool       `json:"online"`
	LastSeen *time.Time `json:"last_seen"`
}

// partners are users sharing an active conversation with user, excluding blocks either way.
func (h *Hub) partners(ctx context.Context, user uuid.UUID) ([]presenceFrame, error) {
	rows, err := h.o.Pool.Query(ctx, `SELECT DISTINCT u.id, u.last_seen_at
		FROM members m1
		JOIN members m2 ON m2.conversation_id = m1.conversation_id AND m2.user_id <> m1.user_id AND m2.left_at IS NULL
		JOIN users u ON u.id = m2.user_id
		WHERE m1.user_id = $1 AND m1.left_at IS NULL AND NOT `+users.BlockedEitherWay("m1.user_id", "m2.user_id"), user)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (presenceFrame, error) {
		var p presenceFrame
		err := r.Scan(&p.UserID, &p.LastSeen)
		return p, err
	})
}

func (h *Hub) broadcastPresence(ctx context.Context, user uuid.UUID, online bool, lastSeen *time.Time) {
	ps, err := h.partners(ctx, user)
	if err != nil {
		h.o.Logger.Error("presence partners", "err", err)
		return
	}
	f := frame{T: "presence", D: presenceFrame{UserID: user, Online: online, LastSeen: lastSeen}}
	for _, p := range ps {
		for _, c := range h.conns(p.UserID) {
			c.push(outItem{f: &f})
		}
	}
}

// sendTyping relays an ephemeral typing signal to the other active, non-blocked members.
func (h *Hub) sendTyping(ctx context.Context, from, conv uuid.UUID) error {
	if ok, _ := h.typing.Allow(from.String() + conv.String()); !ok {
		return nil
	}
	rows, err := h.o.Pool.Query(ctx, `SELECT m2.user_id FROM members m1
		JOIN members m2 ON m2.conversation_id = m1.conversation_id AND m2.user_id <> m1.user_id AND m2.left_at IS NULL
		WHERE m1.conversation_id = $1 AND m1.user_id = $2 AND m1.left_at IS NULL
		  AND NOT `+users.BlockedEitherWay("m1.user_id", "m2.user_id"), conv, from)
	if err != nil {
		return err
	}
	to, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return err
	}
	f := frame{T: "typing", D: map[string]uuid.UUID{"conversation_id": conv, "user_id": from}}
	for _, u := range to {
		for _, c := range h.conns(u) {
			c.push(outItem{f: &f})
		}
	}
	return nil
}

type readyUser struct {
	ID          uuid.UUID `json:"id"`
	Username    string    `json:"username"`
	DisplayName string    `json:"display_name"`
}

func (h *Hub) me(ctx context.Context, id uuid.UUID) (readyUser, error) {
	u := readyUser{ID: id}
	err := h.o.Pool.QueryRow(ctx, `SELECT username, display_name FROM users WHERE id = $1`, id).Scan(&u.Username, &u.DisplayName)
	return u, err
}
