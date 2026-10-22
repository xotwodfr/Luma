package chat

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"luma.folk3s.com/server/internal/auth"
	"luma.folk3s.com/server/internal/httpx"
	"luma.folk3s.com/server/internal/users"
)

const MaxGroupMembers = 256

var (
	errNotGroup   = httpx.E(http.StatusBadRequest, "not_a_group", "That only works in group conversations.")
	errForbidden  = httpx.E(http.StatusForbidden, "forbidden", "Only the group's owner and admins can do that.")
	errGroupFull  = httpx.E(http.StatusConflict, "group_full", "Groups hold at most 256 members.")
	errBadTitle   = httpx.E(http.StatusBadRequest, "invalid_title", "Group titles are 1–80 characters.")
	errBadRole    = httpx.E(http.StatusBadRequest, "invalid_role", `role must be "member", "admin" or "owner".`)
	errSelfTarget = httpx.E(http.StatusBadRequest, "invalid_request", "Use leave to change your own membership.")
)

func validTitle(t string) (string, bool) {
	t = strings.TrimSpace(t)
	n := utf8.RuneCountInString(t)
	return t, utf8.ValidString(t) && n >= 1 && n <= 80 && !strings.ContainsFunc(t, unicode.IsControl)
}

// resolveUsers maps usernames to ids for adding to a group. Unknown, disabled and
// blocked-either-way users all fail the whole request with the same not found, so adds
// can't be used to probe blocks. me and duplicates are dropped.
func resolveUsers(ctx context.Context, tx pgx.Tx, me uuid.UUID, usernames []string) ([]uuid.UUID, error) {
	if len(usernames) > MaxGroupMembers {
		return nil, errGroupFull
	}
	names := make([]string, 0, len(usernames))
	for _, u := range usernames {
		if n := auth.NormalizeUsername(u); !slices.Contains(names, n) {
			names = append(names, n)
		}
	}
	rows, err := tx.Query(ctx, `SELECT u.id FROM users u WHERE u.username = ANY($1::citext[]) AND u.disabled_at IS NULL
		AND u.id <> $2 AND NOT `+users.BlockedEitherWay("u.id", "$2"), names, me)
	if err != nil {
		return nil, err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return nil, err
	}
	var self int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM users WHERE id = $1 AND username = ANY($2::citext[])`, me, names).Scan(&self); err != nil {
		return nil, err
	}
	if len(ids)+self != len(names) {
		return nil, httpx.ErrNotFound
	}
	return ids, nil
}

// CreateGroup makes a group owned by me with the given members (me is added automatically).
func (s *Service) CreateGroup(ctx context.Context, me uuid.UUID, title string, usernames []string) (Conversation, error) {
	title, ok := validTitle(title)
	if !ok {
		return Conversation{}, errBadTitle
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Conversation{}, err
	}
	defer tx.Rollback(ctx)
	others, err := resolveUsers(ctx, tx, me, usernames)
	if err != nil {
		return Conversation{}, err
	}
	if len(others)+1 > MaxGroupMembers {
		return Conversation{}, errGroupFull
	}
	var id uuid.UUID
	if err := tx.QueryRow(ctx, `INSERT INTO conversations (kind, title, created_by) VALUES ('group', $1, $2) RETURNING id`, title, me).Scan(&id); err != nil {
		return Conversation{}, err
	}
	all := append([]uuid.UUID{me}, others...)
	payload, _ := json.Marshal(map[string]any{"conversation_id": id})
	pending := make([]pendingEvent, len(all))
	for i, u := range all {
		pending[i] = pendingEvent{u, "conversation.created", payload}
	}
	events, err := appendEvents(ctx, tx, pending)
	if err != nil {
		return Conversation{}, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO members (conversation_id, user_id, role)
		SELECT $1, u, CASE WHEN u = $2 THEN 'owner' ELSE 'member' END FROM unnest($3::uuid[]) u`, id, me, all); err != nil {
		return Conversation{}, err
	}
	if err := s.commit(ctx, tx, events); err != nil {
		return Conversation{}, err
	}
	return s.Get(ctx, me, id)
}

// groupTx starts a membership change. It locks the conversation row, which serialises every
// membership change and send in that conversation, then reads me's active role and the
// active members. Non-members (including former ones) get not found.
type groupTx struct {
	pgx.Tx
	conv    uuid.UUID
	me      uuid.UUID
	role    string
	members []uuid.UUID // active, sorted
}

func (s *Service) beginGroup(ctx context.Context, me, conv uuid.UUID) (*groupTx, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	g := &groupTx{Tx: tx, conv: conv, me: me}
	kind, role, err := lockConversation(ctx, tx, conv, me)
	g.role = role
	if err == nil && kind != "group" {
		err = errNotGroup
	}
	if err == nil {
		g.members, err = activeMembers(ctx, tx, conv)
	}
	if err != nil {
		tx.Rollback(ctx)
		return nil, err
	}
	return g, nil
}

func (g *groupTx) manager() bool { return g.role == "owner" || g.role == "admin" }

// activeRole returns user's role, or not found if they aren't an active member.
func (g *groupTx) activeRole(ctx context.Context, user uuid.UUID) (string, error) {
	var role string
	err := g.QueryRow(ctx, `SELECT role FROM members WHERE conversation_id = $1 AND user_id = $2 AND left_at IS NULL`, g.conv, user).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", httpx.ErrNotFound
	}
	return role, err
}

// system posts a system message (sender = actor) through the normal send path.
func (g *groupTx) system(ctx context.Context, meta map[string]any, recipients []uuid.UUID, first []pendingEvent) (Message, []Event, error) {
	raw, _ := json.Marshal(meta)
	return post(ctx, g.Tx, g.me, SendInput{ConversationID: g.conv, ClientID: uuid.New(), Kind: "system", Meta: raw}, recipients, first)
}

func membershipEvent(conv, user uuid.UUID, change string) pendingEvent {
	p, _ := json.Marshal(map[string]any{"conversation_id": conv, "user_id": user, "change": change})
	return pendingEvent{user, "membership.updated", p}
}

// finish commits and returns the conversation as me now sees it.
func (s *Service) finish(ctx context.Context, g *groupTx, events []Event) (Conversation, error) {
	if err := s.commit(ctx, g.Tx, events); err != nil {
		return Conversation{}, err
	}
	return s.Get(ctx, g.me, g.conv)
}

// AddMembers adds (or re-adds) users; any active member may add. A re-added user starts a
// new visibility window at the current seq: nothing posted while they were away is visible.
func (s *Service) AddMembers(ctx context.Context, me, conv uuid.UUID, usernames []string) (Conversation, error) {
	g, err := s.beginGroup(ctx, me, conv)
	if err != nil {
		return Conversation{}, err
	}
	defer g.Rollback(ctx)
	ids, err := resolveUsers(ctx, g.Tx, me, usernames)
	if err != nil {
		return Conversation{}, err
	}
	var added []uuid.UUID
	for _, u := range ids {
		if !slices.Contains(g.members, u) {
			added = append(added, u)
		}
	}
	if len(added) == 0 {
		return s.finish(ctx, g, nil)
	}
	if len(g.members)+len(added) > MaxGroupMembers {
		return Conversation{}, errGroupFull
	}
	first := make([]pendingEvent, len(added))
	for i, u := range added {
		first[i] = membershipEvent(conv, u, "added")
	}
	m, events, err := g.system(ctx, map[string]any{"event": "member_added", "actor": me, "users": added}, append(slices.Clone(g.members), added...), first)
	if err != nil {
		return Conversation{}, err
	}
	// The window opens just before the "added" message, so the new member sees it and nothing older.
	if _, err := g.Exec(ctx, `INSERT INTO members (conversation_id, user_id, role, joined_seq, last_read_seq, last_delivered_seq)
		SELECT $1, u, 'member', $3, $3, $3 FROM unnest($2::uuid[]) u
		ON CONFLICT (conversation_id, user_id) DO UPDATE SET role = 'member', joined_at = now(), joined_seq = $3,
			left_at = NULL, left_seq = NULL, title_at_leave = NULL, last_read_seq = $3, last_delivered_seq = $3, archived = false, muted_until = NULL
		WHERE members.left_at IS NOT NULL`, conv, added, m.Seq-1); err != nil {
		return Conversation{}, err
	}
	return s.finish(ctx, g, events)
}

// RemoveMember removes an admin or member; only the owner and admins may, and the owner can't be removed.
// The removed user keeps read access up to and including the removal message.
func (s *Service) RemoveMember(ctx context.Context, me, conv, target uuid.UUID) (Conversation, error) {
	g, err := s.beginGroup(ctx, me, conv)
	if err != nil {
		return Conversation{}, err
	}
	defer g.Rollback(ctx)
	if !g.manager() {
		return Conversation{}, errForbidden
	}
	if target == me {
		return Conversation{}, errSelfTarget
	}
	role, err := g.activeRole(ctx, target)
	if err != nil {
		return Conversation{}, err
	}
	if role == "owner" {
		return Conversation{}, errForbidden
	}
	m, events, err := g.system(ctx, map[string]any{"event": "member_removed", "actor": me, "users": []uuid.UUID{target}},
		g.members, []pendingEvent{membershipEvent(conv, target, "removed")})
	if err != nil {
		return Conversation{}, err
	}
	if _, err := g.Exec(ctx, `UPDATE members SET left_at = now(), left_seq = $3,
		title_at_leave = (SELECT title FROM conversations WHERE id = $1) WHERE conversation_id = $1 AND user_id = $2`, conv, target, m.Seq); err != nil {
		return Conversation{}, err
	}
	return s.finish(ctx, g, events)
}

// Leave removes me from a group. If me owned it, ownership passes to the longest-standing
// admin, else the longest-standing member, so a group with members always has one owner.
func (s *Service) Leave(ctx context.Context, me, conv uuid.UUID) error {
	g, err := s.beginGroup(ctx, me, conv)
	if err != nil {
		return err
	}
	defer g.Rollback(ctx)
	m, events, err := g.system(ctx, map[string]any{"event": "member_left", "actor": me},
		g.members, []pendingEvent{membershipEvent(conv, me, "left")})
	if err != nil {
		return err
	}
	if _, err := g.Exec(ctx, `UPDATE members SET left_at = now(), left_seq = $3,
		title_at_leave = (SELECT title FROM conversations WHERE id = $1) WHERE conversation_id = $1 AND user_id = $2`, conv, me, m.Seq); err != nil {
		return err
	}
	if g.role == "owner" {
		var heir uuid.UUID
		err := g.QueryRow(ctx, `SELECT user_id FROM members WHERE conversation_id = $1 AND left_at IS NULL
			ORDER BY role = 'admin' DESC, joined_at, user_id LIMIT 1`, conv).Scan(&heir)
		if err == nil {
			rest := slices.DeleteFunc(slices.Clone(g.members), func(u uuid.UUID) bool { return u == me })
			_, more, err := g.system(ctx, map[string]any{"event": "owner_transferred", "actor": me, "user": heir},
				rest, []pendingEvent{membershipEvent(conv, heir, "role")})
			if err != nil {
				return err
			}
			events = append(events, more...)
			if _, err := g.Exec(ctx, `UPDATE members SET role = 'owner' WHERE conversation_id = $1 AND user_id = $2`, conv, heir); err != nil {
				return err
			}
		} else if !errors.Is(err, pgx.ErrNoRows) { // no rows: me was the last member
			return err
		}
	}
	return s.commit(ctx, g.Tx, events)
}

// Rename sets the group title; only the owner and admins may.
func (s *Service) Rename(ctx context.Context, me, conv uuid.UUID, title string) (Conversation, error) {
	title, ok := validTitle(title)
	if !ok {
		return Conversation{}, errBadTitle
	}
	g, err := s.beginGroup(ctx, me, conv)
	if err != nil {
		return Conversation{}, err
	}
	defer g.Rollback(ctx)
	if !g.manager() {
		return Conversation{}, errForbidden
	}
	tag, err := g.Exec(ctx, `UPDATE conversations SET title = $2 WHERE id = $1 AND title IS DISTINCT FROM $2`, conv, title)
	if err != nil || tag.RowsAffected() == 0 {
		if err != nil {
			return Conversation{}, err
		}
		return s.finish(ctx, g, nil)
	}
	_, events, err := g.system(ctx, map[string]any{"event": "renamed", "actor": me, "title": title}, g.members, nil)
	if err != nil {
		return Conversation{}, err
	}
	return s.finish(ctx, g, events)
}

// SetRole makes target an admin or member (owner and admins may), or hands ownership to
// target (owner only; the old owner becomes an admin). The owner's own role can't be changed.
func (s *Service) SetRole(ctx context.Context, me, conv, target uuid.UUID, role string) (Conversation, error) {
	if role != "member" && role != "admin" && role != "owner" {
		return Conversation{}, errBadRole
	}
	g, err := s.beginGroup(ctx, me, conv)
	if err != nil {
		return Conversation{}, err
	}
	defer g.Rollback(ctx)
	if !g.manager() || (role == "owner" && g.role != "owner") {
		return Conversation{}, errForbidden
	}
	if target == me {
		return Conversation{}, errSelfTarget
	}
	cur, err := g.activeRole(ctx, target)
	if err != nil {
		return Conversation{}, err
	}
	if cur == "owner" {
		return Conversation{}, errForbidden
	}
	if cur == role {
		return s.finish(ctx, g, nil)
	}
	meta := map[string]any{"event": "role_changed", "actor": me, "user": target, "role": role}
	first := []pendingEvent{membershipEvent(conv, target, "role")}
	if role == "owner" {
		meta = map[string]any{"event": "owner_transferred", "actor": me, "user": target}
		first = append(first, membershipEvent(conv, me, "role"))
	}
	_, events, err := g.system(ctx, meta, g.members, first)
	if err != nil {
		return Conversation{}, err
	}
	if role == "owner" { // demote first: the one-owner index is checked per statement
		if _, err := g.Exec(ctx, `UPDATE members SET role = 'admin' WHERE conversation_id = $1 AND user_id = $2`, conv, me); err != nil {
			return Conversation{}, err
		}
	}
	if _, err := g.Exec(ctx, `UPDATE members SET role = $3 WHERE conversation_id = $1 AND user_id = $2`, conv, target, role); err != nil {
		return Conversation{}, err
	}
	return s.finish(ctx, g, events)
}
