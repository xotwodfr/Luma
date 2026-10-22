package chat

import (
	"context"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"luma.folk3s.com/server/internal/httpx"
)

var errBadSearch = httpx.E(http.StatusBadRequest, "invalid_request", "q must be 1–256 characters and limit 1–50.")

type SearchResult struct {
	ConversationID uuid.UUID `json:"conversation_id"`
	Message        Message   `json:"message"`
	// Headline is plain text with matches wrapped in \u0001…\u0002. It is never HTML.
	Headline string `json:"headline"`
}

// headlineOpts marks matches with \x01 and \x02; any of those already in a body are
// stripped first, so the markers in a headline are always the server's.
const headlineOpts = "StartSel=\x01, StopSel=\x02, MaxWords=24, MinWords=8, ShortWord=2, MaxFragments=1"

// Search finds messages me can see (each member's (joined_seq, left_seq] window, enforced in
// the query), newest first, optionally within one conversation. Blocked senders are not
// filtered: in shared groups their messages are delivered too, and hiding them is the client's call.
func (s *Service) Search(ctx context.Context, me uuid.UUID, q string, conv *uuid.UUID, limit int) ([]SearchResult, error) {
	q = strings.TrimSpace(q)
	if n := utf8.RuneCountInString(q); n == 0 || n > 256 || !utf8.ValidString(q) || strings.ContainsRune(q, 0) || limit < 1 || limit > 50 {
		return nil, errBadSearch
	}
	rows, err := s.pool.Query(ctx, `SELECT msg.id, msg.conversation_id, msg.seq, msg.sender_id, msg.client_id, msg.kind, msg.body, msg.meta, msg.created_at,
			ts_headline('simple', translate(msg.body, E'\x01\x02', ''), query, $4)
		FROM websearch_to_tsquery('simple', $2) query,
			members m JOIN conversations c ON c.id = m.conversation_id
			JOIN messages msg ON msg.conversation_id = m.conversation_id
		WHERE m.user_id = $1 AND ($3::uuid IS NULL OR m.conversation_id = $3)
		  AND msg.seq > m.joined_seq AND msg.seq <= coalesce(m.left_seq, c.last_seq)
		  AND msg.deleted_at IS NULL AND msg.tsv @@ query
		ORDER BY msg.created_at DESC, msg.id LIMIT $5`, me, q, conv, headlineOpts, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (SearchResult, error) {
		var res SearchResult
		m := &res.Message
		err := r.Scan(&m.ID, &m.ConversationID, &m.Seq, &m.SenderID, &m.ClientID, &m.Kind, &m.Body, &m.Meta, &m.CreatedAt, &res.Headline)
		res.ConversationID = m.ConversationID
		return res, err
	})
}
