package chat

import (
	"context"
	"encoding/json"
	"errors"
	"sort"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"luma.folk3s.com/server/internal/httpx"
)

type pendingEvent struct {
	user    uuid.UUID
	typ     string
	payload json.RawMessage
}

// appendEvents writes events into each user's log inside tx. It locks the users' rows in
// id order (the global lock order is conversations → users → members) and takes the next
// event_seq per user, so every user's log is gap-free and written atomically with the change.
func appendEvents(ctx context.Context, tx pgx.Tx, pending []pendingEvent) ([]Event, error) {
	if len(pending) == 0 {
		return nil, nil
	}
	counts := map[uuid.UUID]int64{}
	for _, p := range pending {
		counts[p.user]++
	}
	ids := make([]uuid.UUID, 0, len(counts))
	for id := range counts {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i].String() < ids[j].String() })
	if _, err := tx.Exec(ctx, `SELECT 1 FROM users WHERE id = ANY($1) ORDER BY id FOR UPDATE`, ids); err != nil {
		return nil, err
	}
	next := map[uuid.UUID]int64{}
	for _, id := range ids {
		var last int64
		if err := tx.QueryRow(ctx, `UPDATE users SET event_seq = event_seq + $2 WHERE id = $1 RETURNING event_seq`, id, counts[id]).Scan(&last); err != nil {
			return nil, err
		}
		next[id] = last - counts[id] + 1
	}
	events := make([]Event, len(pending))
	batch := &pgx.Batch{}
	for i, p := range pending {
		events[i] = Event{UserID: p.user, Seq: next[p.user], Type: p.typ, Payload: p.payload}
		next[p.user]++
		batch.Queue(`INSERT INTO user_events (user_id, seq, type, payload) VALUES ($1, $2, $3, $4)`, p.user, events[i].Seq, p.typ, p.payload)
	}
	if err := tx.SendBatch(ctx, batch).Close(); err != nil {
		return nil, err
	}
	return events, nil
}

func activeMembers(ctx context.Context, q queryer, conv uuid.UUID) ([]uuid.UUID, error) {
	rows, err := q.Query(ctx, `SELECT user_id FROM members WHERE conversation_id = $1 AND left_at IS NULL ORDER BY user_id`, conv)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
}

// EventsAfter returns up to limit events with seq > cursor, ascending.
func (s *Service) EventsAfter(ctx context.Context, user uuid.UUID, cursor int64, limit int) ([]Event, error) {
	rows, err := s.pool.Query(ctx, `SELECT seq, type, payload FROM user_events WHERE user_id = $1 AND seq > $2 ORDER BY seq LIMIT $3`, user, cursor, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Event, error) {
		e := Event{UserID: user}
		err := r.Scan(&e.Seq, &e.Type, &e.Payload)
		return e, err
	})
}

// OldestCursor is the smallest cursor that can still be replayed without gaps. A client
// whose cursor is below it must do a full resync.
func (s *Service) OldestCursor(ctx context.Context, user uuid.UUID) (int64, error) {
	var c int64
	err := s.pool.QueryRow(ctx, `SELECT coalesce((SELECT min(seq) FROM user_events WHERE user_id = $1), u.event_seq + 1) - 1
		FROM users u WHERE u.id = $1`, user).Scan(&c)
	return c, err
}

// CurrentCursor is the user's latest event seq.
func (s *Service) CurrentCursor(ctx context.Context, user uuid.UUID) (int64, error) {
	var c int64
	err := s.pool.QueryRow(ctx, `SELECT event_seq FROM users WHERE id = $1`, user).Scan(&c)
	return c, err
}

// MarkRead advances me's read watermark (never backwards, clamped to what me can see).
// Reading implies delivery. Returns the resulting watermark.
func (s *Service) MarkRead(ctx context.Context, me, conv uuid.UUID, seq int64) (int64, error) {
	return s.advance(ctx, me, conv, seq, true)
}

// MarkDelivered advances me's delivered watermark.
func (s *Service) MarkDelivered(ctx context.Context, me, conv uuid.UUID, seq int64) (int64, error) {
	return s.advance(ctx, me, conv, seq, false)
}

func (s *Service) advance(ctx context.Context, me, conv uuid.UUID, seq int64, read bool) (int64, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	members, err := activeMembers(ctx, tx, conv)
	if err != nil {
		return 0, err
	}
	pending := []pendingEvent{}
	// Lock users before the members row (global lock order); appendEvents re-locks harmlessly.
	if _, err := tx.Exec(ctx, `SELECT 1 FROM users WHERE id = ANY($1) ORDER BY id FOR UPDATE`, members); err != nil {
		return 0, err
	}
	var oldRead, oldDelivered, newRead, newDelivered int64
	err = tx.QueryRow(ctx, `WITH cur AS (
			SELECT m.last_read_seq, m.last_delivered_seq, LEAST($3, coalesce(m.left_seq, c.last_seq)) AS target
			FROM members m JOIN conversations c ON c.id = m.conversation_id
			WHERE m.conversation_id = $1 AND m.user_id = $2 FOR UPDATE OF m)
		UPDATE members m SET
			last_read_seq = CASE WHEN $4 THEN GREATEST(cur.last_read_seq, cur.target) ELSE cur.last_read_seq END,
			last_delivered_seq = GREATEST(cur.last_delivered_seq, cur.target)
		FROM cur WHERE m.conversation_id = $1 AND m.user_id = $2
		RETURNING cur.last_read_seq, cur.last_delivered_seq, m.last_read_seq, m.last_delivered_seq`,
		conv, me, seq, read).Scan(&oldRead, &oldDelivered, &newRead, &newDelivered)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, httpx.ErrNotFound
	} else if err != nil {
		return 0, err
	}
	if newRead == oldRead && newDelivered == oldDelivered {
		return pick(read, newRead, newDelivered), tx.Commit(ctx)
	}
	receipt, _ := json.Marshal(map[string]any{"conversation_id": conv, "user_id": me, "last_read_seq": newRead, "last_delivered_seq": newDelivered})
	for _, u := range members {
		if u == me {
			if newRead != oldRead { // keep my other devices' unread counts in sync
				pending = append(pending, pendingEvent{u, "read.updated", receipt})
			}
			continue
		}
		pending = append(pending, pendingEvent{u, "receipt.updated", receipt})
	}
	events, err := appendEvents(ctx, tx, pending)
	if err != nil {
		return 0, err
	}
	return pick(read, newRead, newDelivered), s.commit(ctx, tx, events)
}

func pick(read bool, r, d int64) int64 {
	if read {
		return r
	}
	return d
}
