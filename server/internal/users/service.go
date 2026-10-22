// Package users owns profiles, exact-username discovery and blocking.
package users

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"luma.folk3s.com/server/internal/auth"
	"luma.folk3s.com/server/internal/httpx"
)

// BlockedEitherWay returns a SQL predicate that is true when either user has blocked the other.
// a and b are SQL expressions (placeholders or column refs). Every path that lets one user
// reach another (lookup, DMs, sends, group adds, typing, presence) must use this predicate.
func BlockedEitherWay(a, b string) string {
	return fmt.Sprintf(`EXISTS (SELECT 1 FROM blocks WHERE (blocker_id = %[1]s AND blocked_id = %[2]s) OR (blocker_id = %[2]s AND blocked_id = %[1]s))`, a, b)
}

var errSelfBlock = httpx.E(http.StatusBadRequest, "invalid_request", "You can't block yourself.")

type Profile struct {
	ID          uuid.UUID `json:"id"`
	Username    string    `json:"username"`
	DisplayName string    `json:"display_name"`
	CreatedAt   time.Time `json:"created_at"`
	// Blocked is true when the viewer has blocked this person (so the client can offer Unblock).
	Blocked bool `json:"blocked,omitempty"`
}

type Me struct {
	Profile
	IsAdmin bool `json:"is_admin"`
}

type Service struct{ pool *pgxpool.Pool }

func NewService(pool *pgxpool.Pool) *Service { return &Service{pool} }

func (s *Service) Me(ctx context.Context, id uuid.UUID) (Me, error) {
	var m Me
	err := s.pool.QueryRow(ctx, `SELECT id, username, display_name, created_at, is_admin FROM users WHERE id = $1`, id).
		Scan(&m.ID, &m.Username, &m.DisplayName, &m.CreatedAt, &m.IsAdmin)
	return m, err
}

func (s *Service) UpdateDisplayName(ctx context.Context, id uuid.UUID, name string) (Me, error) {
	d, ok := auth.ValidDisplayName(name)
	if !ok {
		return Me{}, httpx.E(http.StatusBadRequest, "invalid_display_name", "Display names are 1–64 characters.")
	}
	if _, err := s.pool.Exec(ctx, `UPDATE users SET display_name = $1 WHERE id = $2`, d, id); err != nil {
		return Me{}, err
	}
	return s.Me(ctx, id)
}

// Lookup finds a user by exact username (case and surrounding space ignored). Users who are
// disabled or who have blocked the viewer are indistinguishable from nonexistent ones.
func (s *Service) Lookup(ctx context.Context, viewer uuid.UUID, username string) (Profile, error) {
	var p Profile
	err := s.pool.QueryRow(ctx, `SELECT u.id, u.username, u.display_name, u.created_at,
			EXISTS (SELECT 1 FROM blocks WHERE blocker_id = $2 AND blocked_id = u.id)
		FROM users u
		WHERE u.username = $1 AND u.disabled_at IS NULL
		  AND NOT EXISTS (SELECT 1 FROM blocks WHERE blocker_id = u.id AND blocked_id = $2)`,
		auth.NormalizeUsername(username), viewer).
		Scan(&p.ID, &p.Username, &p.DisplayName, &p.CreatedAt, &p.Blocked)
	if errors.Is(err, pgx.ErrNoRows) {
		return Profile{}, httpx.ErrNotFound
	}
	return p, err
}

// Block works even against someone who already blocked the viewer, and does the same work
// (one INSERT … SELECT) whether or not the username exists, so neither status nor timing
// reveals an account.
func (s *Service) Block(ctx context.Context, viewer uuid.UUID, username string) error {
	name := auth.NormalizeUsername(username)
	var self string
	if err := s.pool.QueryRow(ctx, `SELECT username FROM users WHERE id = $1`, viewer).Scan(&self); err != nil {
		return err
	}
	if name == self {
		return errSelfBlock
	}
	_, err := s.pool.Exec(ctx, `INSERT INTO blocks (blocker_id, blocked_id)
		SELECT $1, id FROM users WHERE username = $2 AND id <> $1 ON CONFLICT DO NOTHING`, viewer, name)
	return err
}

func (s *Service) Unblock(ctx context.Context, viewer uuid.UUID, username string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM blocks WHERE blocker_id = $1
		AND blocked_id = (SELECT id FROM users WHERE username = $2)`, viewer, auth.NormalizeUsername(username))
	return err
}
