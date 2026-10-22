package chat

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"luma.folk3s.com/server/internal/httpx"
	"luma.folk3s.com/server/internal/users"
)

const MaxBodyChars = 8000

type SendInput struct {
	ConversationID uuid.UUID       `json:"conversation_id"`
	ClientID       uuid.UUID       `json:"client_id"`
	Kind           string          `json:"kind"`
	Body           string          `json:"body"`
	Meta           json.RawMessage `json:"meta,omitempty"`
}

func (in SendInput) validate() error {
	if in.ClientID == uuid.Nil || in.Kind != "text" || !utf8.ValidString(in.Body) || strings.ContainsRune(in.Body, 0) {
		return errInvalidMessage
	}
	// meta is reserved for server-validated envelopes (attachments arrive in M4); clients can't set it yet.
	if m := strings.TrimSpace(string(in.Meta)); m != "" && m != "{}" && m != "null" {
		return errInvalidMessage
	}
	if n := utf8.RuneCountInString(in.Body); n == 0 || n > MaxBodyChars || strings.TrimSpace(in.Body) == "" {
		return errInvalidMessage
	}
	return nil
}

// Send durably appends a message. Transaction order (spec §11.2):
//
//  1. lock the conversation row (serialises seq allocation; checks active membership)
//  2. return the existing message if (sender, client_id) was already used — no new seq, no events
//  3. refuse DMs where either side blocked the other
//  4. allocate seq = last_seq + 1 and insert the message
//  5. append a message.created event for every active member (users locked in id order)
//  6. advance the sender's read/delivered watermarks
//  7. COMMIT — only then is the notifier (ack + live fan-out) called
//
// created is false when an earlier identical send is returned.
func (s *Service) Send(ctx context.Context, me uuid.UUID, in SendInput) (Message, bool, error) {
	if err := in.validate(); err != nil {
		return Message{}, false, err
	}
	in.Meta = json.RawMessage(`{}`)
	return s.send(ctx, me, in)
}

func (s *Service) send(ctx context.Context, me uuid.UUID, in SendInput) (Message, bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Message{}, false, err
	}
	defer tx.Rollback(ctx)

	var kind string
	err = tx.QueryRow(ctx, `SELECT c.kind FROM conversations c JOIN members m ON m.conversation_id = c.id
		WHERE c.id = $1 AND m.user_id = $2 AND m.left_at IS NULL FOR NO KEY UPDATE OF c`, in.ConversationID, me).Scan(&kind)
	if errors.Is(err, pgx.ErrNoRows) {
		return Message{}, false, httpx.ErrNotFound
	} else if err != nil {
		return Message{}, false, err
	}

	existing, err := queryMessages(ctx, tx, byClientID, me, in.ClientID)
	if err != nil {
		return Message{}, false, err
	}
	if len(existing) == 1 {
		if existing[0].ConversationID != in.ConversationID {
			return Message{}, false, errClientIDReused
		}
		return existing[0], false, nil
	}

	if kind == "direct" {
		var blocked bool
		err := tx.QueryRow(ctx, `SELECT `+users.BlockedEitherWay("$2", "m.user_id")+` FROM members m
			WHERE m.conversation_id = $1 AND m.user_id <> $2`, in.ConversationID, me).Scan(&blocked)
		if err != nil {
			return Message{}, false, err
		}
		if blocked {
			return Message{}, false, errBlocked
		}
	}

	var m Message
	err = tx.QueryRow(ctx, `WITH s AS (
			UPDATE conversations SET last_seq = last_seq + 1, last_activity_at = now() WHERE id = $1 RETURNING last_seq)
		INSERT INTO messages (conversation_id, seq, sender_id, client_id, kind, body, meta)
		SELECT $1, s.last_seq, $2, $3, $4, $5, $6 FROM s
		RETURNING id, conversation_id, seq, sender_id, client_id, kind, body, meta, created_at`,
		in.ConversationID, me, in.ClientID, in.Kind, in.Body, in.Meta).
		Scan(&m.ID, &m.ConversationID, &m.Seq, &m.SenderID, &m.ClientID, &m.Kind, &m.Body, &m.Meta, &m.CreatedAt)
	if isUniqueViolation(err) {
		// The same client_id raced in via a different conversation row lock. Rolling back
		// un-allocates the seq (gap-free); return whichever message won.
		tx.Rollback(ctx)
		return s.existing(ctx, me, in)
	} else if err != nil {
		return Message{}, false, err
	}

	members, err := activeMembers(ctx, tx, in.ConversationID)
	if err != nil {
		return Message{}, false, err
	}
	payload, _ := json.Marshal(map[string]any{"conversation_id": m.ConversationID, "message": m})
	pending := make([]pendingEvent, len(members))
	for i, u := range members {
		pending[i] = pendingEvent{u, "message.created", payload}
	}
	events, err := appendEvents(ctx, tx, pending)
	if err != nil {
		return Message{}, false, err
	}
	if _, err := tx.Exec(ctx, `UPDATE members SET last_read_seq = $3, last_delivered_seq = $3
		WHERE conversation_id = $1 AND user_id = $2`, in.ConversationID, me, m.Seq); err != nil {
		return Message{}, false, err
	}
	if err := s.commit(ctx, tx, events); err != nil {
		return Message{}, false, err
	}
	return m, true, nil
}

func (s *Service) existing(ctx context.Context, me uuid.UUID, in SendInput) (Message, bool, error) {
	ms, err := queryMessages(ctx, s.pool, byClientID, me, in.ClientID)
	if err != nil || len(ms) == 0 {
		return Message{}, false, err
	}
	if ms[0].ConversationID != in.ConversationID {
		return Message{}, false, errClientIDReused
	}
	return ms[0], false, nil
}

type queryer interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

func queryMessages(ctx context.Context, q queryer, sql string, args ...any) ([]Message, error) {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanMessage)
}

const byClientID = `SELECT id, conversation_id, seq, sender_id, client_id, kind, body, meta, created_at
	FROM messages WHERE sender_id = $1 AND client_id = $2`
