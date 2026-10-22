// Package chat owns conversations, membership, message sequencing, watermarks
// and the per-user durable event log used for reconnect replay.
package chat

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"luma.folk3s.com/server/internal/auth"
	"luma.folk3s.com/server/internal/httpx"
	"luma.folk3s.com/server/internal/users"
)

var (
	errBlocked        = httpx.E(http.StatusForbidden, "blocked", "You can't message this person.")
	errInvalidMessage = httpx.E(http.StatusBadRequest, "invalid_message", "Messages need 1–8000 characters of text.")
	errClientIDReused = httpx.E(http.StatusConflict, "client_id_conflict", "This client_id was already used for another conversation.")
	errSelfDM         = httpx.E(http.StatusBadRequest, "invalid_request", "You can't start a conversation with yourself.")
	errBadPage        = httpx.E(http.StatusBadRequest, "invalid_request", "limit must be 1–100 and only one of before_seq and after_seq may be set.")
)

type Message struct {
	ID             uuid.UUID       `json:"id"`
	ConversationID uuid.UUID       `json:"conversation_id"`
	Seq            int64           `json:"seq"`
	SenderID       uuid.UUID       `json:"sender_id"`
	ClientID       uuid.UUID       `json:"client_id"`
	Kind           string          `json:"kind"`
	Body           string          `json:"body"`
	Meta           json.RawMessage `json:"meta"`
	CreatedAt      time.Time       `json:"created_at"`
}

type Member struct {
	UserID           uuid.UUID `json:"user_id"`
	Username         string    `json:"username"`
	DisplayName      string    `json:"display_name"`
	Role             string    `json:"role"`
	LastReadSeq      int64     `json:"last_read_seq"`
	LastDeliveredSeq int64     `json:"last_delivered_seq"`
}

// Conversation is one conversation as seen by one member.
type Conversation struct {
	ID             uuid.UUID `json:"id"`
	Kind           string    `json:"kind"`
	Title          *string   `json:"title"`
	CreatedAt      time.Time `json:"created_at"`
	LastSeq        int64     `json:"last_seq"`
	LastActivityAt time.Time `json:"last_activity_at"`
	LastReadSeq    int64     `json:"last_read_seq"`
	Unread         int64     `json:"unread"`
	Muted          bool      `json:"muted"`
	Archived       bool      `json:"archived"`
	Left           bool      `json:"left"`
	LastMessage    *Message  `json:"last_message"`
	Members        []Member  `json:"members"`
}

// Event is one durable entry in a user's event log. Payload is the type-specific body.
type Event struct {
	UserID  uuid.UUID       `json:"-"`
	Seq     int64           `json:"seq"`
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"data"`
}

// Notifier receives events only after the transaction that wrote them has committed.
type Notifier interface{ Committed(events []Event) }

type Service struct {
	pool     *pgxpool.Pool
	notifier Notifier
	// failBeforeCommit, when set, aborts write transactions right before COMMIT (tests only).
	failBeforeCommit error
}

func NewService(pool *pgxpool.Pool, n Notifier) *Service { return &Service{pool: pool, notifier: n} }

// SetNotifier replaces the notifier (the realtime hub is built after the service).
func (s *Service) SetNotifier(n Notifier) { s.notifier = n }

// FailBeforeCommitForTesting makes every write transaction fail just before COMMIT.
func FailBeforeCommitForTesting(s *Service, err error) { s.failBeforeCommit = err }

func (s *Service) commit(ctx context.Context, tx pgx.Tx, events []Event) error {
	if s.failBeforeCommit != nil {
		return s.failBeforeCommit
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	if s.notifier != nil && len(events) > 0 {
		s.notifier.Committed(events)
	}
	return nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// OpenDirect returns the 1:1 conversation between me and username, creating it if needed.
// A missing, disabled or blocked-either-way user is reported as not found.
//
// allowNew is consulted only when a new conversation would be created (nil allows); its error is returned.
func (s *Service) OpenDirect(ctx context.Context, me uuid.UUID, username string, allowNew func() error) (Conversation, error) {
	var other uuid.UUID
	err := s.pool.QueryRow(ctx, `SELECT u.id FROM users u WHERE u.username = $1 AND u.disabled_at IS NULL
		AND NOT `+users.BlockedEitherWay("u.id", "$2"), auth.NormalizeUsername(username), me).Scan(&other)
	if errors.Is(err, pgx.ErrNoRows) {
		return Conversation{}, httpx.ErrNotFound
	} else if err != nil {
		return Conversation{}, err
	}
	if other == me {
		return Conversation{}, errSelfDM
	}
	a, b := me, other
	if a.String() > b.String() {
		a, b = b, a
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Conversation{}, err
	}
	defer tx.Rollback(ctx)
	var id uuid.UUID
	err = tx.QueryRow(ctx, `SELECT conversation_id FROM direct_pairs WHERE user_a = $1 AND user_b = $2`, a, b).Scan(&id)
	if err == nil {
		tx.Rollback(ctx)
		return s.Get(ctx, me, id)
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return Conversation{}, err
	}
	if allowNew != nil {
		if err := allowNew(); err != nil {
			return Conversation{}, err
		}
	}
	if err := tx.QueryRow(ctx, `INSERT INTO conversations (kind, created_by) VALUES ('direct', $1) RETURNING id`, me).Scan(&id); err != nil {
		return Conversation{}, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO direct_pairs (user_a, user_b, conversation_id) VALUES ($1, $2, $3)`, a, b, id); err != nil {
		if isUniqueViolation(err) { // a concurrent open won the race; use its conversation
			tx.Rollback(ctx)
			var existing uuid.UUID
			if err := s.pool.QueryRow(ctx, `SELECT conversation_id FROM direct_pairs WHERE user_a = $1 AND user_b = $2`, a, b).Scan(&existing); err != nil {
				return Conversation{}, err
			}
			return s.Get(ctx, me, existing)
		}
		return Conversation{}, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO members (conversation_id, user_id, role) VALUES ($1, $2, 'member'), ($1, $3, 'member')`, id, me, other); err != nil {
		return Conversation{}, err
	}
	payload, _ := json.Marshal(map[string]any{"conversation_id": id})
	events, err := appendEvents(ctx, tx, []pendingEvent{{me, "conversation.created", payload}, {other, "conversation.created", payload}})
	if err != nil {
		return Conversation{}, err
	}
	if err := s.commit(ctx, tx, events); err != nil {
		return Conversation{}, err
	}
	return s.Get(ctx, me, id)
}

const conversationCols = `c.id, c.kind, c.title, c.created_at, c.last_seq, c.last_activity_at,
	m.last_read_seq, m.joined_seq, coalesce(m.left_seq, c.last_seq), m.left_at IS NOT NULL,
	coalesce(m.muted_until > now(), false), m.archived,
	lm.id, lm.seq, lm.sender_id, lm.client_id, lm.kind, lm.body, lm.meta, lm.created_at`

// The last visible message is bounded by the member's (joined_seq, left_seq] window.
const conversationFrom = `FROM members m JOIN conversations c ON c.id = m.conversation_id
	LEFT JOIN LATERAL (SELECT * FROM messages WHERE conversation_id = c.id AND seq > m.joined_seq
		AND seq <= coalesce(m.left_seq, c.last_seq) ORDER BY seq DESC LIMIT 1) lm ON true`

func scanConversation(row pgx.Row) (Conversation, error) {
	var c Conversation
	var joined, visibleLast int64
	var lm struct {
		ID, Sender, Client *uuid.UUID
		Seq                *int64
		Kind, Body         *string
		Meta               []byte
		At                 *time.Time
	}
	err := row.Scan(&c.ID, &c.Kind, &c.Title, &c.CreatedAt, &c.LastSeq, &c.LastActivityAt,
		&c.LastReadSeq, &joined, &visibleLast, &c.Left, &c.Muted, &c.Archived,
		&lm.ID, &lm.Seq, &lm.Sender, &lm.Client, &lm.Kind, &lm.Body, &lm.Meta, &lm.At)
	if err != nil {
		return c, err
	}
	c.LastSeq = visibleLast
	c.Unread = max(0, visibleLast-max(c.LastReadSeq, joined))
	if lm.ID != nil {
		c.LastMessage = &Message{ID: *lm.ID, ConversationID: c.ID, Seq: *lm.Seq, SenderID: *lm.Sender, ClientID: *lm.Client,
			Kind: *lm.Kind, Body: *lm.Body, Meta: lm.Meta, CreatedAt: *lm.At}
	}
	return c, nil
}

// List returns every conversation the user belongs or belonged to, most recent activity first.
func (s *Service) List(ctx context.Context, me uuid.UUID) ([]Conversation, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+conversationCols+` `+conversationFrom+`
		WHERE m.user_id = $1 ORDER BY c.last_activity_at DESC, c.id`, me)
	if err != nil {
		return nil, err
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Conversation, error) { return scanConversation(r) })
	if err != nil {
		return nil, err
	}
	return list, s.attachMembers(ctx, list)
}

// Get returns one conversation as seen by me; non-members get not found.
func (s *Service) Get(ctx context.Context, me, id uuid.UUID) (Conversation, error) {
	c, err := scanConversation(s.pool.QueryRow(ctx, `SELECT `+conversationCols+` `+conversationFrom+`
		WHERE m.user_id = $1 AND c.id = $2`, me, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Conversation{}, httpx.ErrNotFound
	} else if err != nil {
		return Conversation{}, err
	}
	list := []Conversation{c}
	err = s.attachMembers(ctx, list)
	return list[0], err
}

func (s *Service) attachMembers(ctx context.Context, list []Conversation) error {
	if len(list) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, len(list))
	idx := map[uuid.UUID]int{}
	for i, c := range list {
		ids[i] = c.ID
		idx[c.ID] = i
		list[i].Members = []Member{}
	}
	rows, err := s.pool.Query(ctx, `SELECT m.conversation_id, u.id, u.username, u.display_name, m.role, m.last_read_seq, m.last_delivered_seq
		FROM members m JOIN users u ON u.id = m.user_id
		WHERE m.conversation_id = ANY($1) AND m.left_at IS NULL ORDER BY m.joined_at, u.username`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var conv uuid.UUID
		var m Member
		if err := rows.Scan(&conv, &m.UserID, &m.Username, &m.DisplayName, &m.Role, &m.LastReadSeq, &m.LastDeliveredSeq); err != nil {
			return err
		}
		list[idx[conv]].Members = append(list[idx[conv]].Members, m)
	}
	return rows.Err()
}

// History pages through the messages me may see: (joined_seq, left_seq]. Results are ascending by seq.
// beforeSeq and afterSeq are exclusive bounds; with neither, the latest page is returned.
func (s *Service) History(ctx context.Context, me, conv uuid.UUID, beforeSeq, afterSeq int64, limit int) ([]Message, error) {
	if limit < 1 || limit > 100 || (beforeSeq > 0 && afterSeq > 0) || beforeSeq < 0 || afterSeq < 0 {
		return nil, errBadPage
	}
	var lo, hi int64
	err := s.pool.QueryRow(ctx, `SELECT m.joined_seq, coalesce(m.left_seq, c.last_seq) FROM members m
		JOIN conversations c ON c.id = m.conversation_id WHERE m.conversation_id = $1 AND m.user_id = $2`, conv, me).Scan(&lo, &hi)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.ErrNotFound
	} else if err != nil {
		return nil, err
	}
	var rows pgx.Rows
	const cols = `SELECT id, conversation_id, seq, sender_id, client_id, kind, body, meta, created_at FROM messages
		WHERE conversation_id = $1 AND seq > $2 AND seq <= $3`
	switch {
	case afterSeq > 0:
		rows, err = s.pool.Query(ctx, cols+` AND seq > $4 ORDER BY seq ASC LIMIT $5`, conv, lo, hi, afterSeq, limit)
	case beforeSeq > 0:
		rows, err = s.pool.Query(ctx, cols+` AND seq < $4 ORDER BY seq DESC LIMIT $5`, conv, lo, hi, beforeSeq, limit)
	default:
		rows, err = s.pool.Query(ctx, cols+` ORDER BY seq DESC LIMIT $4`, conv, lo, hi, limit)
	}
	if err != nil {
		return nil, err
	}
	msgs, err := pgx.CollectRows(rows, scanMessage)
	if err != nil {
		return nil, err
	}
	if afterSeq == 0 { // fetched descending; return ascending
		for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
			msgs[i], msgs[j] = msgs[j], msgs[i]
		}
	}
	return msgs, nil
}

func scanMessage(r pgx.CollectableRow) (Message, error) {
	var m Message
	err := r.Scan(&m.ID, &m.ConversationID, &m.Seq, &m.SenderID, &m.ClientID, &m.Kind, &m.Body, &m.Meta, &m.CreatedAt)
	return m, err
}
