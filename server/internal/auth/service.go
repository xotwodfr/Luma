// Package auth owns accounts' credentials: registration, login, sessions and invites.
package auth

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"luma.folk3s.com/server/internal/httpx"
)

const (
	// RevokeChannel is the Postgres NOTIFY channel carrying revoked session ids.
	RevokeChannel = "luma_revoke"

	SessionIdleExpiry   = 90 * 24 * time.Hour
	InviteTTL           = 7 * 24 * time.Hour
	MaxOpenUserInvites  = 5
	lastSeenGranularity = time.Minute
)

var (
	usernameRE = regexp.MustCompile(`^[a-z0-9_]{3,24}$`)

	errInvalidCredentials = httpx.E(http.StatusUnauthorized, "invalid_credentials", "Username or password is incorrect.")
	errInvalidInvite      = httpx.E(http.StatusBadRequest, "invalid_invite", "This invite code is not valid. Ask for a new one.")
	errInvalidUsername    = httpx.E(http.StatusBadRequest, "invalid_username", "Usernames are 3–24 characters: letters, numbers and underscores.")
	errUsernameTaken      = httpx.E(http.StatusConflict, "username_unavailable", "That username is taken.")
	errInvalidPassword    = httpx.E(http.StatusBadRequest, "invalid_password", "Passwords are 10–256 characters.")
	errInvalidDisplayName = httpx.E(http.StatusBadRequest, "invalid_display_name", "Display names are 1–64 characters.")
	errRegistrationClosed = httpx.E(http.StatusForbidden, "registration_closed", "Registration is closed on this server.")
	errAccountDisabled    = httpx.E(http.StatusForbidden, "account_disabled", "This account has been disabled.")
	errInviteLimit        = httpx.E(http.StatusTooManyRequests, "invite_limit", "You already have 5 unused invites.")
)

// Principal is the authenticated caller of a request.
type Principal struct {
	UserID    uuid.UUID
	SessionID uuid.UUID
}

type User struct {
	ID          uuid.UUID `json:"id"`
	Username    string    `json:"username"`
	DisplayName string    `json:"display_name"`
	CreatedAt   time.Time `json:"created_at"`
}

type Session struct {
	ID         uuid.UUID `json:"id"`
	DeviceName string    `json:"device_name"`
	Platform   string    `json:"platform"`
	CreatedAt  time.Time `json:"created_at"`
	LastSeenAt time.Time `json:"last_seen_at"`
	Current    bool      `json:"current"`
}

type Service struct {
	pool         *pgxpool.Pool
	registration string
	// OnRevoke is called after commit for every session that was revoked, so live sockets can be closed.
	OnRevoke func(sessionID uuid.UUID)
}

func NewService(pool *pgxpool.Pool, registration string) *Service {
	return &Service{pool: pool, registration: registration}
}

// NormalizeUsername is for lookups: tolerant of case and surrounding whitespace.
func NormalizeUsername(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// ValidUsername is for registration: case-folds but rejects whitespace instead of trimming it.
func ValidUsername(s string) (string, bool) {
	u := strings.ToLower(s)
	return u, usernameRE.MatchString(u)
}

func ValidDisplayName(s string) (string, bool) {
	d := strings.TrimSpace(s)
	n := utf8.RuneCountInString(d)
	return d, n >= 1 && n <= 64
}

func validPassword(pw string) bool { n := len(pw); return n >= 10 && n <= 256 }

type RegisterInput struct {
	Invite      string `json:"invite"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Password    string `json:"password"`
	DeviceName  string `json:"device_name"`
	Platform    string `json:"platform"`
}

type Device struct {
	Name, Platform, IP string
}

func (d Device) clean() Device {
	if utf8.RuneCountInString(d.Name) > 64 {
		d.Name = string([]rune(d.Name)[:64])
	}
	switch d.Platform {
	case "ios", "macos", "android", "windows", "linux", "web", "test":
	default:
		d.Platform = ""
	}
	return d
}

// Register creates an account and its first session. With invite registration, a taken
// username is only reported after a valid invite was presented (limits enumeration).
func (s *Service) Register(ctx context.Context, in RegisterInput, dev Device) (string, User, error) {
	if s.registration == "closed" {
		return "", User{}, errRegistrationClosed
	}
	username, ok := ValidUsername(in.Username)
	if !ok {
		return "", User{}, errInvalidUsername
	}
	display, ok := ValidDisplayName(in.DisplayName)
	if !ok {
		return "", User{}, errInvalidDisplayName
	}
	if !validPassword(in.Password) {
		return "", User{}, errInvalidPassword
	}
	hash, err := HashPassword(in.Password)
	if err != nil {
		return "", User{}, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", User{}, err
	}
	defer tx.Rollback(ctx)

	var inviteHash []byte
	if s.registration == "invite" {
		inviteHash = hashInvite(in.Invite)
		var found bool
		err := tx.QueryRow(ctx, `SELECT true FROM invites
			WHERE code_hash = $1 AND used_at IS NULL AND (expires_at IS NULL OR expires_at > now())
			FOR UPDATE`, inviteHash).Scan(&found)
		if errors.Is(err, pgx.ErrNoRows) {
			return "", User{}, errInvalidInvite
		} else if err != nil {
			return "", User{}, err
		}
	}

	var u User
	err = tx.QueryRow(ctx, `INSERT INTO users (username, display_name, password_hash) VALUES ($1, $2, $3)
		RETURNING id, username, display_name, created_at`, username, display, hash).
		Scan(&u.ID, &u.Username, &u.DisplayName, &u.CreatedAt)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return "", User{}, errUsernameTaken
	} else if err != nil {
		return "", User{}, err
	}
	if inviteHash != nil {
		if _, err := tx.Exec(ctx, `UPDATE invites SET used_by = $1, used_at = now() WHERE code_hash = $2`, u.ID, inviteHash); err != nil {
			return "", User{}, err
		}
	}
	token, err := createSession(ctx, tx, u.ID, dev.clean())
	if err != nil {
		return "", User{}, err
	}
	return token, u, tx.Commit(ctx)
}

type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func createSession(ctx context.Context, q querier, userID uuid.UUID, dev Device) (string, error) {
	plain, hash := NewToken()
	var ip any
	if dev.IP != "" {
		ip = dev.IP
	}
	var id uuid.UUID
	err := q.QueryRow(ctx, `INSERT INTO sessions (user_id, token_hash, device_name, platform, ip)
		VALUES ($1, $2, $3, $4, $5) RETURNING id`, userID, hash, dev.Name, dev.Platform, ip).Scan(&id)
	return plain, err
}

// Login verifies credentials with equalised timing and returns a new session token.
func (s *Service) Login(ctx context.Context, username, password string, dev Device) (string, User, error) {
	var u User
	var hash string
	var disabled *time.Time
	err := s.pool.QueryRow(ctx, `SELECT id, username, display_name, created_at, password_hash, disabled_at
		FROM users WHERE username = $1`, NormalizeUsername(username)).
		Scan(&u.ID, &u.Username, &u.DisplayName, &u.CreatedAt, &hash, &disabled)
	if errors.Is(err, pgx.ErrNoRows) {
		DummyVerify()
		return "", User{}, errInvalidCredentials
	} else if err != nil {
		return "", User{}, err
	}
	ok, err := VerifyPassword(hash, password)
	if err != nil {
		return "", User{}, err
	}
	if !ok {
		return "", User{}, errInvalidCredentials
	}
	if disabled != nil {
		return "", User{}, errAccountDisabled
	}
	token, err := createSession(ctx, s.pool, u.ID, dev.clean())
	return token, u, err
}

// Authenticate resolves a bearer token to a live session, enforcing revocation,
// disabled accounts and the sliding idle expiry.
func (s *Service) Authenticate(ctx context.Context, token string) (Principal, error) {
	if token == "" || len(token) > 128 {
		return Principal{}, httpx.ErrUnauthorized
	}
	var p Principal
	var lastSeen time.Time
	var disabled *time.Time
	err := s.pool.QueryRow(ctx, `SELECT s.id, s.user_id, s.last_seen_at, u.disabled_at
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = $1 AND s.revoked_at IS NULL`, HashToken(token)).
		Scan(&p.SessionID, &p.UserID, &lastSeen, &disabled)
	if errors.Is(err, pgx.ErrNoRows) {
		return Principal{}, httpx.ErrUnauthorized
	} else if err != nil {
		return Principal{}, err
	}
	if disabled != nil {
		return Principal{}, httpx.ErrUnauthorized
	}
	if time.Since(lastSeen) > SessionIdleExpiry {
		_, _ = s.revoke(ctx, `id = $1`, p.SessionID)
		return Principal{}, httpx.ErrUnauthorized
	}
	if time.Since(lastSeen) > lastSeenGranularity {
		if _, err := s.pool.Exec(ctx, `UPDATE sessions SET last_seen_at = now() WHERE id = $1`, p.SessionID); err != nil {
			return Principal{}, err
		}
	}
	return p, nil
}

// revoke marks matching live sessions revoked and notifies OnRevoke after commit.
func (s *Service) revoke(ctx context.Context, where string, args ...any) (int, error) {
	rows, err := s.pool.Query(ctx, `UPDATE sessions SET revoked_at = now() WHERE revoked_at IS NULL AND `+where+` RETURNING id`, args...)
	if err != nil {
		return 0, err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return 0, err
	}
	if len(ids) > 0 {
		// Other processes (the admin CLI, future server instances) close their sockets via NOTIFY.
		if _, err := s.pool.Exec(ctx, `SELECT pg_notify('`+RevokeChannel+`', x::text) FROM unnest($1::uuid[]) x`, ids); err != nil {
			return len(ids), err
		}
	}
	if s.OnRevoke != nil {
		for _, id := range ids {
			s.OnRevoke(id)
		}
	}
	return len(ids), nil
}

func (s *Service) Logout(ctx context.Context, p Principal) error {
	_, err := s.revoke(ctx, `id = $1`, p.SessionID)
	return err
}

func (s *Service) ListSessions(ctx context.Context, p Principal) ([]Session, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, device_name, platform, created_at, last_seen_at FROM sessions
		WHERE user_id = $1 AND revoked_at IS NULL ORDER BY last_seen_at DESC`, p.UserID)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Session, error) {
		var s Session
		err := r.Scan(&s.ID, &s.DeviceName, &s.Platform, &s.CreatedAt, &s.LastSeenAt)
		return s, err
	})
	for i := range out {
		out[i].Current = out[i].ID == p.SessionID
	}
	return out, err
}

// RevokeSession revokes one of the caller's own sessions; another user's session looks not found.
func (s *Service) RevokeSession(ctx context.Context, p Principal, id uuid.UUID) error {
	n, err := s.revoke(ctx, `id = $1 AND user_id = $2`, id, p.UserID)
	if err == nil && n == 0 {
		return httpx.ErrNotFound
	}
	return err
}

func (s *Service) RevokeOthers(ctx context.Context, p Principal) error {
	_, err := s.revoke(ctx, `user_id = $1 AND id <> $2`, p.UserID, p.SessionID)
	return err
}

// ChangePassword verifies the current password, stores the new hash and revokes every other session.
func (s *Service) ChangePassword(ctx context.Context, p Principal, current, next string) error {
	if !validPassword(next) {
		return errInvalidPassword
	}
	var hash string
	if err := s.pool.QueryRow(ctx, `SELECT password_hash FROM users WHERE id = $1`, p.UserID).Scan(&hash); err != nil {
		return err
	}
	ok, err := VerifyPassword(hash, current)
	if err != nil {
		return err
	}
	if !ok {
		return errInvalidCredentials
	}
	newHash, err := HashPassword(next)
	if err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx, `UPDATE users SET password_hash = $1 WHERE id = $2`, newHash, p.UserID); err != nil {
		return err
	}
	return s.RevokeOthers(ctx, p)
}

// CreateInvite issues a single-use invite on behalf of a user, within their quota.
func (s *Service) CreateInvite(ctx context.Context, p Principal) (string, time.Time, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", time.Time{}, err
	}
	defer tx.Rollback(ctx)
	// Lock the user row so concurrent requests can't both pass the quota check.
	if _, err := tx.Exec(ctx, `SELECT 1 FROM users WHERE id = $1 FOR NO KEY UPDATE`, p.UserID); err != nil {
		return "", time.Time{}, err
	}
	var open int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM invites WHERE created_by = $1 AND used_at IS NULL
		AND (expires_at IS NULL OR expires_at > now())`, p.UserID).Scan(&open); err != nil {
		return "", time.Time{}, err
	}
	if open >= MaxOpenUserInvites {
		return "", time.Time{}, errInviteLimit
	}
	code := newInviteCode()
	expires := time.Now().Add(InviteTTL).UTC().Truncate(time.Second)
	if _, err := tx.Exec(ctx, `INSERT INTO invites (code_hash, created_by, expires_at) VALUES ($1, $2, $3)`,
		hashInvite(code), p.UserID, expires); err != nil {
		return "", time.Time{}, err
	}
	return code, expires, tx.Commit(ctx)
}

// CreateAdminInvite issues a non-expiring invite with no owner (admin CLI, first user).
func (s *Service) CreateAdminInvite(ctx context.Context) (string, error) {
	code := newInviteCode()
	_, err := s.pool.Exec(ctx, `INSERT INTO invites (code_hash) VALUES ($1)`, hashInvite(code))
	return code, err
}

// DisableUser blocks login and revokes every session of username (admin CLI).
func (s *Service) DisableUser(ctx context.Context, username string) error {
	var id uuid.UUID
	err := s.pool.QueryRow(ctx, `UPDATE users SET disabled_at = coalesce(disabled_at, now()) WHERE username = $1 RETURNING id`,
		NormalizeUsername(username)).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return httpx.ErrNotFound
	} else if err != nil {
		return err
	}
	_, err = s.revoke(ctx, `user_id = $1`, id)
	return err
}
